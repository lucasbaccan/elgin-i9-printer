package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

func cmdServe() {
	port := os.Getenv("ELGIN_API_PORT")
	if port == "" {
		port = "8000"
	}

	log.Printf("[STARTUP] Elgin Print Server")
	log.Printf("[STARTUP] Device: %s", LP)
	log.Printf("[STARTUP] Device presente: %v", devicePresent())

	go watchdog()

	mux := http.NewServeMux()
	mux.HandleFunc("/", logMiddleware(handleRoot))
	mux.HandleFunc("/health", logMiddleware(handleHealth))
	mux.HandleFunc("/ping", logMiddleware(handlePing))
	mux.HandleFunc("/print", logMiddleware(handlePrint))
	mux.HandleFunc("/feed", logMiddleware(handleFeed))
	mux.HandleFunc("/cut", logMiddleware(handleCut))
	mux.HandleFunc("/qr", logMiddleware(handleQR))
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

// handlePing imprime "pong" na impressora (teste real de ponta a ponta) e
// responde "pong" no corpo (texto puro, sem JSON). Sai SEM o modo compacto:
// o respiro antes do corte destaca o cupom de teste.
func handlePing(w http.ResponseWriter, r *http.Request) {
	log.Printf("[PING] Iniciando teste de pong")
	if !devicePresent() {
		writeErr(w, 503, fmt.Sprintf("device %s não encontrado", LP))
		return
	}
	log.Printf("[PING] Device presente, montando cupom")
	dados, err := montarCupom("", []Linha{{Texto: "pong", Alinhamento: "centro"}})
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
// (obrigatório) e tamanho (módulo 1..8, padrão 4). Usa o mesmo gerador da
// impressão, então o preview bate com o papel.
func handleQR(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if strings.TrimSpace(text) == "" {
		writeErr(w, 400, "parâmetro 'text' obrigatório")
		return
	}
	mod := qrModPadrao
	if v := r.URL.Query().Get("tamanho"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 8 {
			mod = n
		}
	}
	log.Printf("[QR] Gerando QR para: %s (módulo=%d)", text[:20], mod)
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
		"servico": "Elgin I9 Print API",
		"versao":  "1.0.0",
		"endpoints": map[string]string{
			"GET /":              "Web UI (navegador) / esta documentação (curl/API)",
			"GET /health":        "status da impressora (device)",
			"GET /ping":          "pong (health-check simples)",
			"POST /print":        "imprime cupom personalizado (corpo JSON)",
			"POST /feed":         "avança papel (corpo {\"linhas\": N})",
			"POST /cut":          "aciona a guilhotina",
			"GET /qr":            "renderiza um QR como PNG (sem imprimir) — ?text=...&tamanho=1..8",
			"GET /test-grayscale": "imagem de teste (degradê 16 tons) para validar impressão de cinza",
			"POST /test-print":   "imprime o degradê de teste (sem argumentos)",
		},
		"alinhamentos": map[string]string{
			"esquerda": "texto colado na margem esquerda",
			"centro":   "texto centralizado (padrão)",
			"direita":  "texto colado na margem direita",
		},
		"fontes": map[string]string{
			"normal": "Fonte A 12x24 - 48 colunas por linha (padrão)",
			"larga":  "largura 2x - 24 colunas por linha (bom para títulos)",
		},
		"tipos_de_linha": map[string]string{
			"texto":  "padrão (tipo ausente = texto) - campos texto/alinhamento/fonte/negrito/linha",
			"imagem": "imprime uma imagem (campo 'imagem': base64 PNG/JPEG/GIF, com ou sem prefixo data:)",
			"qr":     "gera um QR code do campo 'qr' (texto/URL) e imprime como imagem (fallback GS v 0)",
		},
		"imagem": map[string]string{
			"campo":         "imagem",
			"como_funciona": "a imagem é convertida para 1-bit (halftone Bayer por padrão - preserva cinza melhor), reduzida para no máximo 576 dots de largura (80mm) mantendo a proporção e impressa via GS v 0",
			"alinhamento":   "esquerda | centro (padrão) | direita - respeitado com respiro em branco até a largura do papel",
			"dither":        "bayer (padrão - halftone/meio-tom, melhor para fotos/cinza) | floyd (difusão de erro - mais suave, melhor para gráficos/logos)",
		},
		"qr_code": map[string]any{
			"campos":        map[string]string{"qr": "conteúdo (URL/texto)", "qr_tamanho": "tamanho do módulo 1..8 (padrão 4)"},
			"como_funciona": "QR gerado no servidor (correção M) e impresso como imagem GS v 0 (fallback — não depende do suporte a GS ( k da i9). Se não couber em 576 dots, o módulo é reduzido automaticamente",
			"exemplo":       map[string]any{"tipo": "qr", "qr": "https://exemplo.com", "qr_tamanho": 4},
		},
		"preenchimento_de_linha": map[string]any{
			"campo":         "linha",
			"como_funciona": "com linha=true, o campo texto é repetido até preencher a linha inteira (48 colunas na fonte normal, 24 na larga)",
			"exemplo":       map[string]any{"texto": "-X", "linha": true, "alinhamento": "esquerda"},
			"resultado":     preencher("-X", WidthNormal),
		},
		"quebra_de_linha": "textos maiores que a largura da linha (48 normal / 24 larga) são quebrados automaticamente em várias linhas — nada é truncado",
		"compact":         "campo compact (bool) no POST /print: true/ausente = corte rente à última linha (modo compacto, padrão); false = respiro de 3 linhas antes do corte",
		"exemplo_completo": map[string]any{
			"titulo": "PEDIDO #123",
			"linhas": []map[string]any{
				{"texto": "=", "alinhamento": "esquerda", "linha": true},
				{"texto": "1x Hamburguer", "alinhamento": "esquerda"},
				{"texto": "2x Refrigerante", "alinhamento": "esquerda"},
				{"texto": "TOTAL: R$ 45,00", "alinhamento": "direita", "fonte": "larga"},
				{"tipo": "imagem", "imagem": "<base64>", "alinhamento": "centro", "dither": "bayer"},
				{"tipo": "qr", "qr": "https://exemplo.com", "alinhamento": "centro"},
				{"texto": "=", "alinhamento": "esquerda", "linha": true},
			},
		},
		"como_chamar": map[string]string{
			"health":        "curl http://<host>:8000/health",
			"print":         "curl -X POST http://<host>:8000/print -H 'Content-Type: application/json' -d '{...}'",
			"feed":          "curl -X POST http://<host>:8000/feed -H 'Content-Type: application/json' -d '{\"linhas\": 5}'",
			"cut":           "curl -X POST http://<host>:8000/cut",
			"test-grayscale": "curl http://<host>:8000/test-grayscale > teste.png",
			"test-print":    "curl -X POST http://<host>:8000/test-print",
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
		"ok": true,
		"mensagem": "Degradê de teste impresso! Tons esperados: 16 (branco 0 → preto 15). Conte quantos tons DIFERENTES você consegue ver na impressão e avise.",
	})
}
