package main

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
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
	if err := enviar(montarCupom("", []Linha{{Texto: "pong", Alinhamento: "centro"}}), true, feedCortePadrao); err != nil {
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
	if err := enviar(montarCupom(titulo, cupom.Linhas), true, feedCorte); err != nil {
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
		"preenchimento_de_linha": map[string]any{
			"campo":         "linha",
			"como_funciona": "com linha=true, o campo texto é repetido até preencher a linha inteira (48 colunas na fonte normal, 24 na larga)",
			"exemplo":       map[string]any{"texto": "-X", "linha": true, "alinhamento": "esquerda"},
			"resultado":     preencher("-X", WidthNormal),
		},
		"quebra_de_linha": "textos maiores que a largura da linha (48 normal / 24 larga) são quebrados automaticamente em várias linhas — nada é truncado",
		"compact": "campo compact (bool) no POST /print: true/ausente = corte rente à última linha (modo compacto, padrão); false = respiro de 3 linhas antes do corte",
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
