package main

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

//go:embed webui/index.html
var webUI []byte

// Documentação para agentes de IA, servida em /llms.txt e /openapi.json. O
// marcador {{BASE_URL}} é trocado pela URL com que o cliente acessou o servidor.
//
//go:embed webui/llms.txt
var llmsTxt []byte

//go:embed webui/openapi.json
var openapiJSON []byte

// exemplosFS guarda os exemplos (exemplos.json + imagens) do menu "Exemplos" e dos endpoints /exemplos.
//
//go:embed webui/exemplos
var exemplosFS embed.FS

// swaggerFS guarda o Swagger UI (swagger-ui-dist, Apache-2.0) para a página /docs/
// funcionar sem internet. O index.html carrega /openapi.json.
//
//go:embed webui/swagger
var swaggerFS embed.FS

func cmdServe() {
	port := os.Getenv("ELGIN_API_PORT")
	if port == "" {
		port = "8000"
	}

	log.Printf("[STARTUP] Elgin Print Server")
	log.Printf("[STARTUP] Device: %s", LP)
	log.Printf("[STARTUP] Device presente: %v", devicePresent())

	if d := os.Getenv("ELGIN_DATA_DIR"); d != "" {
		dataDir = d
	}
	if mb, err := strconv.Atoi(os.Getenv("ELGIN_IMG_MAX_MB")); err == nil && mb > 0 {
		imgMaxTotal = int64(mb) << 20
	}
	log.Printf("[STARTUP] Imagens compartilhadas: %s (validade %d dias, limite %d MB)", imgDir(), int(imgTTL/(24*time.Hour)), imgMaxTotal>>20)
	iniciarLimpezaImagens()

	go watchdog()

	mux := http.NewServeMux()
	mux.HandleFunc("/", logMiddleware(handleRoot))
	mux.Handle("/docs/", http.StripPrefix("/docs/", cacheLongo(http.FileServerFS(subSwagger()))))
	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/imagens", logMiddleware(handleImagens))
	mux.HandleFunc("/imagens/", logMiddleware(handleImagens))
	mux.HandleFunc("/exemplos", logMiddleware(handleExemplos))
	mux.HandleFunc("/exemplos/", logMiddleware(handleExemplos))
	mux.HandleFunc("/llms.txt", logMiddleware(handleLlmsTxt))
	mux.HandleFunc("/openapi.json", logMiddleware(handleOpenAPI))
	mux.HandleFunc("/health", logMiddleware(handleHealth))
	mux.HandleFunc("/ping", logMiddleware(handlePing))
	mux.HandleFunc("/print", logMiddleware(handlePrint))
	mux.HandleFunc("/feed", logMiddleware(handleFeed))
	mux.HandleFunc("/cut", logMiddleware(handleCut))
	mux.HandleFunc("/qr", logMiddleware(handleQR))
	mux.HandleFunc("/imagem/preview", logMiddleware(handleImagemPreview))
	mux.HandleFunc("/qr/info", logMiddleware(handleQRInfo))
	mux.HandleFunc("/test-grayscale", logMiddleware(handleTestGrayscale))
	mux.HandleFunc("/test-print", logMiddleware(handleTestPrint))

	addr := "0.0.0.0:" + port
	log.Printf("[STARTUP] Servidor escutando em http://%s", addr)
	log.Printf("[STARTUP] Web UI em http://<seu-ip>:%s/", port)
	log.Printf("[STARTUP] API Docs em http://<seu-ip>:%s/ (via curl)", port)

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("[ERROR] Falha ao iniciar servidor: %v", err)
	}
}

func logMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ip := getClientIP(r)
		log.Printf("[REQUEST] %s %s %s (de %s)", r.Method, r.URL.Path, r.Proto, ip)

		wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}
		next(wrapped, r)

		duration := time.Since(start).Milliseconds()
		log.Printf("[RESPONSE] %s %s -> %d (%.0fms)", r.Method, r.URL.Path, wrapped.statusCode, float64(duration))
	}
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

// watchdog observa o device e loga transições de conexão. Em VM com passthrough
// USB nativo (usb0: host=20d1:7008), o QEMU re-apega o device automaticamente
// quando a impressora religa — aqui só registramos o evento para o journal e
// mantemos o /health refletindo o estado real a cada request.
func watchdog() {
	last := devicePresent()
	for range time.Tick(2 * time.Second) {
		now := devicePresent()
		if now != last {
			if now {
				log.Printf("impressora reconectada em %s", LP)
			} else {
				log.Printf("impressora desconectada (device %s sumiu)", LP)
			}
			last = now
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	log.Printf("[ERROR] %d: %s", code, msg)
	writeJSON(w, code, map[string]any{"ok": false, "detail": msg})
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// Navegadores mandam Accept: text/html -> Web UI. curl/API (Accept */*)
	// recebem a documentação JSON, preservando o comportamento antigo do GET /.
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(webUI)
		return
	}
	writeJSON(w, 200, apiDocs())
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	ok := devicePresent()
	status := "indisponivel"
	if ok {
		status = "pronta"
	}
	writeJSON(w, 200, map[string]any{"ok": ok, "device": LP, "status": status})
}

// handlePing imprime "PONG!" em fonte gigante (5 caracteres de 96 dots = 83% da
// largura do papel) na impressora (teste real de ponta a ponta) e
// responde "pong" no corpo (texto puro, sem JSON). Sai SEM o modo compacto:
// o respiro antes do corte destaca o cupom de teste.
func handlePing(w http.ResponseWriter, r *http.Request) {
	log.Printf("[PING] Iniciando teste de pong")
	if !devicePresent() {
		writeErr(w, 503, fmt.Sprintf("device %s não encontrado", LP))
		return
	}
	log.Printf("[PING] Device presente, montando cupom")
	dados, err := montarCupom("", []Linha{{Texto: "PONG!", Alinhamento: "centro", Fonte: "gigante", Negrito: true}})
	if err != nil {
		writeErr(w, 400, fmt.Sprintf("erro ao montar cupom: %v", err))
		return
	}
	log.Printf("[PING] Cupom montado (%d bytes), enviando para impressora", len(dados))
	if err := enviar(dados, true, feedCortePadrao); err != nil {
		writeErr(w, 503, fmt.Sprintf("erro ao enviar para impressora: %v", err))
		return
	}
	log.Printf("[PING] Pong enviado com sucesso!")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("pong"))
}

func handlePrint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	var cupom Cupom
	if err := json.NewDecoder(r.Body).Decode(&cupom); err != nil {
		writeErr(w, 400, fmt.Sprintf("JSON inválido: %v", err))
		return
	}
	executarPrint(w, cupom)
}

// executarPrint valida, monta e envia um cupom à impressora (POST /print e
// POST /exemplos/{id}/print) e escreve a resposta HTTP.
func executarPrint(w http.ResponseWriter, cupom Cupom) {
	log.Printf("[PRINT] Recebido cupom: titulo=%v, linhas=%d", cupom.Titulo, len(cupom.Linhas))
	if cupom.Titulo == nil && len(cupom.Linhas) == 0 {
		writeErr(w, 400, "envie ao menos um título ou uma linha")
		return
	}
	titulo := ""
	if cupom.Titulo != nil {
		titulo = *cupom.Titulo
	}
	// modo compacto é o padrão (true/ausente): corte rente. compact=false dá
	// o respiro (feedCortePadrao linhas) antes do corte.
	feedCorte := 0
	if cupom.Compact != nil && !*cupom.Compact {
		feedCorte = feedCortePadrao
	}
	if !devicePresent() {
		writeErr(w, 503, fmt.Sprintf("device %s não encontrado", LP))
		return
	}
	dados, err := montarCupom(titulo, cupom.Linhas)
	if err != nil {
		writeErr(w, 400, fmt.Sprintf("erro ao montar cupom: %v", err))
		return
	}
	log.Printf("[PRINT] Cupom montado (%d bytes), enviando", len(dados))
	if err := enviar(dados, true, feedCorte); err != nil {
		writeErr(w, 503, fmt.Sprintf("erro ao enviar: %v", err))
		return
	}
	log.Printf("[PRINT] Sucesso! %d linha(s) impressa(s)", len(cupom.Linhas))
	writeJSON(w, 200, map[string]any{"ok": true, "linhas": len(cupom.Linhas)})
}

func handleFeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	var body struct {
		Linhas int `json:"linhas"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body.Linhas <= 0 {
		body.Linhas = 3
	}
	log.Printf("[FEED] Avançando %d linha(s)", body.Linhas)
	if !devicePresent() {
		writeErr(w, 503, fmt.Sprintf("device %s não encontrado", LP))
		return
	}
	if err := enviar(feedBytes(body.Linhas), false, 0); err != nil {
		writeErr(w, 503, fmt.Sprintf("erro ao avançar papel: %v", err))
		return
	}
	log.Printf("[FEED] Sucesso!")
	writeJSON(w, 200, map[string]any{"ok": true, "linhas": body.Linhas})
}

func handleCut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	log.Printf("[CUT] Acionando guilhotina")
	if !devicePresent() {
		writeErr(w, 503, fmt.Sprintf("device %s não encontrado", LP))
		return
	}
	if err := enviar(cutBytes(), false, 0); err != nil {
		writeErr(w, 503, fmt.Sprintf("erro ao acionar guilhotina: %v", err))
		return
	}
	log.Printf("[CUT] Sucesso!")
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleQR renderiza um QR code como PNG (sem imprimir) — usado pelo preview
// da Web UI e útil para testar a geração via curl. Parâmetros: text
// (obrigatório) e tamanho (módulo 3..23, padrão 14; fora disso é ajustado sem erro). Usa o mesmo gerador da
// impressão, então o preview bate com o papel.
func handleQR(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if strings.TrimSpace(text) == "" {
		writeErr(w, 400, "parâmetro 'text' obrigatório")
		return
	}
	mod := qrModPadrao
	if v := r.URL.Query().Get("tamanho"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			mod = n // qrPNG ajusta para 3..23 (sem erro)
		}
	}
	preview := []rune(text)
	if len(preview) > 20 {
		preview = preview[:20]
	}
	log.Printf("[QR] Gerando QR para: %s (módulo=%d)", string(preview), mod)
	png, err := qrPNG(text, mod)
	if err != nil {
		writeErr(w, 400, fmt.Sprintf("erro ao gerar QR: %v", err))
		return
	}
	log.Printf("[QR] QR gerado com sucesso (%d bytes)", len(png))
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(png)
}

func apiDocs() map[string]any {
	return map[string]any{
		"servico":      "Elgin I9 Print API",
		"versao":       "1.0.0",
		"docs_para_ia": map[string]string{"llms.txt": "/llms.txt", "openapi": "/openapi.json", "swagger": "/docs/"},
		"endpoints": map[string]string{
			"GET /":                     "Web UI (navegador) / esta documentação (curl/API)",
			"GET /llms.txt":             "guia completo em Markdown para agentes de IA",
			"GET /openapi.json":         "especificação OpenAPI 3.1",
			"GET /docs/":                "Swagger UI: documentação interativa de todos os endpoints",
			"POST /imagens":             "salva uma imagem por 7 dias (id = hash do conteúdo; reenviar renova o prazo)",
			"GET /imagens/{id}":         "devolve a imagem salva (404 se expirou)",
			"GET /exemplos":             "lista os cupons de exemplo prontos",
			"GET /exemplos/{id}":        "o cupom do exemplo (corpo pronto para o POST /print, não imprime)",
			"POST /exemplos/{id}/print": "imprime o exemplo",
			"GET /health":               "status da impressora (device)",
			"GET /ping":                 "pong (health-check simples)",
			"POST /print":               "imprime cupom personalizado (corpo JSON)",
			"POST /feed":                "avança papel (corpo {\"linhas\": N})",
			"POST /cut":                 "aciona a guilhotina",
			"GET /qr":                   "renderiza um QR como PNG (sem imprimir) — ?text=...&tamanho=3..23",
			"GET /test-grayscale":       "imagem de teste (degradê 16 tons) para validar impressão de cinza",
			"POST /test-print":          "imprime o degradê de teste (sem argumentos)",
		},
		"alinhamentos": map[string]string{
			"esquerda": "texto colado na margem esquerda",
			"centro":   "texto centralizado (padrão)",
			"direita":  "texto colado na margem direita",
		},
		"fontes": map[string]string{
			"normal":  "Fonte A 12x24 - 48 colunas por linha (padrão)",
			"larga":   "largura 2x - 24 colunas por linha (bom para títulos)",
			"gigante": "largura 8x e altura 4x (caracteres de 96x96 dots) - 6 colunas por linha (senhas, destaques)",
		},
		"tipos_de_linha": map[string]string{
			"texto":  "padrão (tipo ausente = texto) - campos texto/alinhamento/fonte/negrito/linha",
			"imagem": "imprime uma imagem (campo 'imagem': base64 PNG/JPEG/GIF, com ou sem prefixo data:)",
			"qr":     "gera um QR code do campo 'qr' (texto/URL) e imprime como imagem (fallback GS v 0)",
		},
		"imagem": map[string]string{
			"campo":         "imagem",
			"como_funciona": "a imagem é convertida para 1-bit (dither Atkinson), reduzida para no máximo 576 dots de largura (80mm) mantendo a proporção e impressa via GS v 0",
			"alinhamento":   "esquerda | centro (padrão) | direita - respeitado com respiro em branco até a largura do papel",
			"ajustes":       "opcionais: largura (8..576 dots), brilho/contraste/meios_tons (-100..100), nitidez (0..100, padrão 30), auto_contraste (padrão true), inverter",
		},
		"qr_code": map[string]any{
			"campos":        map[string]string{"qr": "conteúdo (URL/texto)", "qr_tamanho": "tamanho do módulo 3..23 (padrão 14; fora disso é ajustado sem erro)", "alinhamento": "esquerda | centro (padrão) | direita"},
			"como_funciona": "QR gerado no servidor (correção M) e impresso como imagem GS v 0 (fallback — não depende do suporte a GS ( k da i9). Se não couber em 576 dots, o módulo é reduzido automaticamente",
			"exemplo":       map[string]any{"tipo": "qr", "qr": "https://exemplo.com", "qr_tamanho": 14, "alinhamento": "centro"},
		},
		"preenchimento_de_linha": map[string]any{
			"campo":         "linha",
			"como_funciona": "com linha=true, o campo texto é repetido até preencher a linha inteira (48 colunas na fonte normal, 24 na larga, 6 na gigante)",
			"exemplo":       map[string]any{"texto": "-X", "linha": true, "alinhamento": "esquerda"},
			"resultado":     preencher("-X", WidthNormal),
		},
		"quebra_de_linha": "textos maiores que a largura da linha (48 normal / 24 larga / 6 gigante) são quebrados automaticamente em várias linhas — nada é truncado",
		"compact":         "campo compact (bool) no POST /print: true/ausente = corte rente à última linha (modo compacto, padrão); false = respiro de 3 linhas antes do corte",
		"exemplo_completo": map[string]any{
			"titulo": "PEDIDO #123",
			"linhas": []map[string]any{
				{"texto": "=", "alinhamento": "esquerda", "linha": true},
				{"texto": "1x Hamburguer", "alinhamento": "esquerda"},
				{"texto": "2x Refrigerante", "alinhamento": "esquerda"},
				{"texto": "TOTAL: R$ 45,00", "alinhamento": "direita", "fonte": "larga"},
				{"tipo": "imagem", "imagem": "<base64>", "alinhamento": "centro"},
				{"tipo": "qr", "qr": "https://exemplo.com", "alinhamento": "centro"},
				{"texto": "=", "alinhamento": "esquerda", "linha": true},
			},
		},
		"como_chamar": map[string]string{
			"health":         "curl http://<host>:8000/health",
			"print":          "curl -X POST http://<host>:8000/print -H 'Content-Type: application/json' -d '{...}'",
			"feed":           "curl -X POST http://<host>:8000/feed -H 'Content-Type: application/json' -d '{\"linhas\": 5}'",
			"cut":            "curl -X POST http://<host>:8000/cut",
			"test-grayscale": "curl http://<host>:8000/test-grayscale > teste.png",
			"test-print":     "curl -X POST http://<host>:8000/test-print",
		},
	}
}

// handleTestGrayscale serve uma imagem PNG com degradê de 16 tons de cinza
// para validar quantos níveis a impressora consegue reproduzir
func handleTestGrayscale(w http.ResponseWriter, r *http.Request) {
	log.Printf("[TEST-GRAYSCALE] Gerando imagem de teste (degradê 16 tons)")
	png, err := gerarTesteDegradé()
	if err != nil {
		writeErr(w, 500, fmt.Sprintf("erro ao gerar degradê: %v", err))
		return
	}
	log.Printf("[TEST-GRAYSCALE] Imagem gerada (%d bytes)", len(png))
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(png)
}

// handleTestPrint imprime o degradê de teste (16 tons de cinza) para validação
func handleTestPrint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	log.Printf("[TEST-PRINT] Iniciando teste de degradê (16 tons)")
	if !devicePresent() {
		writeErr(w, 503, fmt.Sprintf("device %s não encontrado", LP))
		return
	}

	png, err := gerarTesteDegradé()
	if err != nil {
		writeErr(w, 500, fmt.Sprintf("erro ao gerar degradê: %v", err))
		return
	}

	img, err := decodeImagem(fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(png)))
	if err != nil {
		writeErr(w, 500, fmt.Sprintf("erro ao decodificar imagem: %v", err))
		return
	}

	log.Printf("[TEST-PRINT] Imagem decodificada, convertendo para GS v 0")
	dados, err := imagemParaGSv0(img, "centro")
	if err != nil {
		writeErr(w, 400, fmt.Sprintf("erro ao processar imagem: %v", err))
		return
	}

	log.Printf("[TEST-PRINT] Enviando para impressora (%d bytes)", len(dados))
	if err := enviar(dados, true, feedCortePadrao); err != nil {
		writeErr(w, 503, fmt.Sprintf("erro ao enviar: %v", err))
		return
	}

	log.Printf("[TEST-PRINT] Degradê impresso! Conte quantos tons diferentes você consegue ver (0-15)")
	writeJSON(w, 200, map[string]any{
		"ok":       true,
		"mensagem": "Degradê de teste impresso! Tons esperados: 16 (branco 0 → preto 15). Conte quantos tons DIFERENTES você consegue ver na impressão e avise.",
	})
}

// handleQRInfo (GET /qr/info?text=...) devolve {"modulos": N}: lado do QR em
// módulos (sem borda). Largura impressa = modulos * tamanho dots (máx 576).
func handleQRInfo(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if strings.TrimSpace(text) == "" {
		writeErr(w, 400, "parâmetro 'text' obrigatório")
		return
	}
	n, err := qrModulos(text)
	if err != nil {
		writeErr(w, 400, fmt.Sprintf("erro ao gerar QR: %v", err))
		return
	}
	writeJSON(w, 200, map[string]any{"modulos": n, "papel_dots": dotWidth})
}

// handleImagemPreview (POST /imagem/preview, corpo {"imagem": base64|data URL,
// e os ajustes de imagem) devolve um PNG preto e branco com o resultado do
// dither — como a imagem sairá na impressora. Não imprime nada.
func handleImagemPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "use POST")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	var in struct {
		Linha        // imagem + ajustes (mesmos campos do bloco de imagem de /print)
		EscalaPx int `json:"escala_px"` // px da tela equivalentes a 576 dots (reduz a saída ao tamanho do preview)
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, fmt.Sprintf("JSON inválido: %v", err))
		return
	}
	img, err := decodeImagem(in.Imagem)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	out, _, err := imagemPreviewPNG(img, ajustesDe(in.Linha), in.EscalaPx)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(out)
}

// baseURL monta a URL pela qual o cliente chegou (esquema + Host), para os
// exemplos de curl da documentação já virem com o endereço certo. Hosts com
// caracteres estranhos caem num placeholder (o valor entra em JSON e Markdown).
func baseURL(r *http.Request) string {
	esquema := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		esquema = "https"
	}
	host := r.Host
	if host == "" || strings.Trim(host, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-:[]") != "" {
		return "http://<host>:8000"
	}
	return esquema + "://" + host
}

func servirDoc(w http.ResponseWriter, r *http.Request, corpo []byte, tipo string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeErr(w, http.StatusMethodNotAllowed, "use GET")
		return
	}
	w.Header().Set("Content-Type", tipo)
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(strings.ReplaceAll(string(corpo), "{{BASE_URL}}", baseURL(r))))
}

// handleLlmsTxt serve o guia em Markdown para agentes de IA (convenção llms.txt).
func handleLlmsTxt(w http.ResponseWriter, r *http.Request) {
	servirDoc(w, r, llmsTxt, "text/markdown; charset=utf-8")
}

// handleOpenAPI serve a especificação OpenAPI 3.1 da API.
func handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	servirDoc(w, r, openapiJSON, "application/json; charset=utf-8")
}

// subSwagger devolve os arquivos do Swagger UI enraizados em webui/swagger.
func subSwagger() fs.FS {
	sub, err := fs.Sub(swaggerFS, "webui/swagger")
	if err != nil {
		log.Fatalf("[ERROR] embed swagger: %v", err)
	}
	return sub
}

// cacheLongo deixa o navegador guardar os arquivos (o bundle tem ~1,4 MB).
func cacheLongo(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		h.ServeHTTP(w, r)
	})
}
