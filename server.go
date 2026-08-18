package main

import (
	_ "embed"
	"encoding/json"
	"log"
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

	go watchdog()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/ping", handlePing)
	mux.HandleFunc("/print", handlePrint)
	mux.HandleFunc("/feed", handleFeed)
	mux.HandleFunc("/cut", handleCut)
	mux.HandleFunc("/qr", handleQR)

	addr := "0.0.0.0:" + port
	log.Printf("elgin-print serve em http://%s (device %s)", addr, LP)
	log.Printf("Web UI em http://<host>:%s/", port)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
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
	dados, err := montarCupom("", []Linha{{Texto: "pong", Alinhamento: "centro"}})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := enviar(dados, true, feedCortePadrao); err != nil {
		writeErr(w, 503, err.Error())
		return
	}
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
		writeErr(w, 400, "JSON inválido: "+err.Error())
		return
	}
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
	dados, err := montarCupom(titulo, cupom.Linhas)
	if err != nil {
		// erro de conteúdo (imagem/QR inválidos) é 400, não falha do device
		writeErr(w, 400, err.Error())
		return
	}
	if err := enviar(dados, true, feedCorte); err != nil {
		writeErr(w, 503, err.Error())
		return
	}
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
	if err := enviar(feedBytes(body.Linhas), false, 0); err != nil {
		writeErr(w, 503, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "linhas": body.Linhas})
}

func handleCut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	if err := enviar(cutBytes(), false, 0); err != nil {
		writeErr(w, 503, err.Error())
		return
	}
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
	png, err := qrPNG(text, mod)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(png)
}

func apiDocs() map[string]any {
	return map[string]any{
		"servico": "Elgin I9 Print API",
		"versao":  "1.0.0",
		"endpoints": map[string]string{
			"GET /":       "Web UI (navegador) / esta documentação (curl/API)",
			"GET /health": "status da impressora (device)",
			"GET /ping":   "pong (health-check simples)",
			"POST /print": "imprime cupom personalizado (corpo JSON)",
			"POST /feed":  "avança papel (corpo {\"linhas\": N})",
			"POST /cut":   "aciona a guilhotina",
			"GET /qr":     "renderiza um QR como PNG (sem imprimir) — ?text=...&tamanho=1..8",
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
			"como_funciona": "a imagem é convertida para 1-bit (dither Floyd-Steinberg), reduzida para no máximo 576 dots de largura (80mm) mantendo a proporção e impressa via GS v 0",
			"alinhamento":   "esquerda | centro (padrão) | direita - respeitado com respiro em branco até a largura do papel",
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
				{"texto": "=", "alinhamento": "esquerda", "linha": true},
			},
		},
		"como_chamar": map[string]string{
			"health": "curl http://<host>:8000/health",
			"print":  "curl -X POST http://<host>:8000/print -H 'Content-Type: application/json' -d '{...}'",
			"feed":   "curl -X POST http://<host>:8000/feed -H 'Content-Type: application/json' -d '{\"linhas\": 5}'",
			"cut":    "curl -X POST http://<host>:8000/cut",
		},
	}
}
