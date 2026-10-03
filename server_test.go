package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHealthSemDevice(t *testing.T) {
	orig := LP
	LP = "/caminho/inexistente/lp0"
	defer func() { LP = orig }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("health deveria retornar 200, veio %d", rec.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["ok"] != false || m["status"] != "indisponivel" {
		t.Fatalf("sem device, health deveria ser ok=false/indisponivel, veio %v", m)
	}
}

func TestHealthComDevice(t *testing.T) {
	f := filepath.Join(t.TempDir(), "lp0")
	if err := os.WriteFile(f, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	orig := LP
	LP = f
	defer func() { LP = orig }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handleHealth(rec, req)

	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["ok"] != true || m["status"] != "pronta" {
		t.Fatalf("com device, health deveria ser ok=true/pronta, veio %v", m)
	}
}

func TestPrintEnviaEImprime(t *testing.T) {
	var captured []byte
	var cut bool
	orig := enviar
	enviar = func(dados []byte, cortar bool, feedCorte int) error { captured = dados; cut = cortar; return nil }
	defer func() { enviar = orig }()

	body := `{"titulo":"PEDIDO #1","linhas":[{"texto":"1x Hamburguer","alinhamento":"esquerda"}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(body))
	handlePrint(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("print deveria retornar 200, veio %d (%s)", rec.Code, rec.Body.String())
	}
	if len(captured) == 0 {
		t.Fatal("print não enviou bytes para a impressora")
	}
	if !cut {
		t.Fatal("print deveria acionar o corte (cortar=true)")
	}
}

func TestPrintVazioRetorna400(t *testing.T) {
	orig := enviar
	enviar = func(dados []byte, cortar bool, feedCorte int) error { return nil }
	defer func() { enviar = orig }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(`{"linhas":[]}`))
	handlePrint(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("print vazio deveria retornar 400, veio %d", rec.Code)
	}
}

func TestPrintJSONInvalidoRetorna400(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(`{nao é json`))
	handlePrint(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("JSON inválido deveria retornar 400, veio %d", rec.Code)
	}
}

func TestFeedEndpoint(t *testing.T) {
	var captured []byte
	orig := enviar
	enviar = func(dados []byte, cortar bool, feedCorte int) error { captured = dados; return nil }
	defer func() { enviar = orig }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/feed", strings.NewReader(`{"linhas":5}`))
	handleFeed(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("feed deveria retornar 200, veio %d", rec.Code)
	}
	if !bytes.Equal(captured, []byte("\x1b\x64\x05")) {
		t.Fatalf("feed(5) deveria enviar ESC d 5, enviou %x", captured)
	}
}

func TestPingEndpoint(t *testing.T) {
	var captured []byte
	orig := enviar
	enviar = func(dados []byte, cortar bool, feedCorte int) error { captured = dados; return nil }
	defer func() { enviar = orig }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	handlePing(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ping deveria retornar 200, veio %d", rec.Code)
	}
	if rec.Body.String() != "pong" {
		t.Fatalf("ping deveria responder 'pong', veio %q", rec.Body.String())
	}
	if !bytes.Contains(captured, []byte("PONG!")) {
		t.Fatalf("ping deveria imprimir 'PONG!', bytes enviados: %q", captured)
	}
}

func TestPrintComBlocosGraficos(t *testing.T) {
	var captured []byte
	orig := enviar
	enviar = func(dados []byte, cortar bool, feedCorte int) error { captured = dados; return nil }
	defer func() { enviar = orig }()

	// QR + imagem no mesmo cupom: os dois viram raster GS v 0 no buffer.
	raw := branca(40, 20)
	b64 := base64.StdEncoding.EncodeToString(raw)
	body := `{"titulo":"T","linhas":[{"tipo":"qr","qr":"https://exemplo.com"},{"tipo":"imagem","imagem":"` + b64 + `"}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(body))
	handlePrint(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("print com blocos gráficos deveria retornar 200, veio %d (%s)", rec.Code, rec.Body.String())
	}
	// dois blocos gráficos = dois cabeçalhos GS v 0
	if n := bytes.Count(captured, []byte{0x1d, 0x76, 0x30, 0x00}); n != 2 {
		t.Fatalf("esperado 2 rasters GS v 0 (QR + imagem), veio %d", n)
	}
}

func TestPrintImagemInvalidaRetorna400(t *testing.T) {
	orig := enviar
	enviar = func(dados []byte, cortar bool, feedCorte int) error { return nil }
	defer func() { enviar = orig }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(`{"linhas":[{"tipo":"imagem","imagem":"!!!"}]}`))
	handlePrint(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("imagem inválida deveria retornar 400, veio %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestCutEndpoint(t *testing.T) {
	var captured []byte
	orig := enviar
	enviar = func(dados []byte, cortar bool, feedCorte int) error { captured = dados; return nil }
	defer func() { enviar = orig }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/cut", nil)
	handleCut(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("cut deveria retornar 200, veio %d", rec.Code)
	}
	if !bytes.Equal(captured, cutCmd) {
		t.Fatalf("cut deveria enviar GS V 66 0, enviou %x", captured)
	}
}

func TestRootNegociacaoHTMLvsJSON(t *testing.T) {
	// curl manda Accept */* -> JSON (preserva GET / antigo)
	recJSON := httptest.NewRecorder()
	reqJSON := httptest.NewRequest(http.MethodGet, "/", nil)
	reqJSON.Header.Set("Accept", "*/*")
	handleRoot(recJSON, reqJSON)
	if !strings.HasPrefix(strings.TrimSpace(recJSON.Body.String()), "{") {
		t.Fatalf("Accept */* deveria retornar JSON, veio: %s", recJSON.Body.String()[:60])
	}

	// navegador manda Accept text/html -> Web UI
	recHTML := httptest.NewRecorder()
	reqHTML := httptest.NewRequest(http.MethodGet, "/", nil)
	reqHTML.Header.Set("Accept", "text/html,application/xhtml+xml")
	handleRoot(recHTML, reqHTML)
	if !strings.Contains(recHTML.Header().Get("Content-Type"), "text/html") {
		t.Fatal("Accept text/html deveria retornar a Web UI")
	}
	if !strings.Contains(recHTML.Body.String(), "<html") {
		t.Fatal("Web UI deveria conter markup HTML")
	}
}

func TestQREndpoint(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/qr?text=https://exemplo.com&tamanho=4", nil)
	handleQR(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("qr deveria retornar 200, veio %d (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("qr deveria retornar image/png, veio %q", ct)
	}
	// PNG válido começa com a assinatura 89 50 4E 47
	if len(rec.Body.Bytes()) < 8 || !bytes.HasPrefix(rec.Body.Bytes(), []byte{0x89, 0x50, 0x4e, 0x47}) {
		t.Fatal("corpo do QR não é um PNG válido")
	}
}

func TestQREndpointSemTexto(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/qr", nil)
	handleQR(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("qr sem texto deveria retornar 400, veio %d", rec.Code)
	}
}

func TestRootPathDesconhecido404(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nao-existe", nil)
	handleRoot(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("path desconhecido deveria retornar 404, veio %d", rec.Code)
	}
}

func TestLlmsTxtEOpenAPI(t *testing.T) {
	for _, c := range []struct {
		path, tipo string
		h          http.HandlerFunc
	}{{"/llms.txt", "text/markdown", handleLlmsTxt}, {"/openapi.json", "application/json", handleOpenAPI}} {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		req.Host = "impressora.local:8000"
		rec := httptest.NewRecorder()
		c.h(rec, req)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), c.tipo) {
			t.Fatalf("%s: status %d, content-type %q", c.path, rec.Code, rec.Header().Get("Content-Type"))
		}
		corpo := rec.Body.String()
		if strings.Contains(corpo, "{{BASE_URL}}") || !strings.Contains(corpo, "http://impressora.local:8000") {
			t.Fatalf("%s: o marcador {{BASE_URL}} deveria virar a URL de acesso", c.path)
		}
		if c.path == "/llms.txt" { // todo exemplo precisa estar citado no guia (evita doc desatualizada)
			lista, _ := carregarExemplos()
			for _, e := range lista {
				if !strings.Contains(corpo, "`"+e.ID+"`") {
					t.Errorf("exemplo %q não está citado em llms.txt", e.ID)
				}
			}
		}
		if c.path == "/openapi.json" {
			var doc struct {
				Paths map[string]any `json:"paths"`
			}
			if err := json.Unmarshal([]byte(corpo), &doc); err != nil {
				t.Fatalf("openapi.json inválido: %v", err)
			}
			// todo endpoint registrado no servidor deve estar documentado
			for _, p := range []string{"/health", "/print", "/feed", "/cut", "/ping", "/qr", "/qr/info",
				"/imagem/preview", "/test-grayscale", "/test-print", "/llms.txt", "/openapi.json", "/docs/", "/exemplos", "/exemplos/{id}", "/exemplos/{id}/print", "/imagens", "/imagens/{id}"} {
				if _, ok := doc.Paths[p]; !ok {
					t.Errorf("endpoint %s não está no openapi.json", p)
				}
			}
		}
	}
	// Host com caracteres estranhos não pode vazar para a documentação
	req := httptest.NewRequest(http.MethodGet, "/llms.txt", nil)
	req.Host = `x"><script>`
	rec := httptest.NewRecorder()
	handleLlmsTxt(rec, req)
	if strings.Contains(rec.Body.String(), "<script>") {
		t.Fatal("Host inválido não deveria ser refletido na documentação")
	}
}

func TestSwaggerUIServidoDoBinario(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/docs/", http.StripPrefix("/docs/", cacheLongo(http.FileServerFS(subSwagger()))))
	for _, c := range []struct{ path, contem string }{
		{"/docs/", "swagger-ui-bundle.js"},
		{"/docs/swagger-ui.css", ".swagger-ui"},
		{"/docs/swagger-ui-bundle.js", "SwaggerUIBundle"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), c.contem) {
			t.Fatalf("%s: status %d, esperava conter %q", c.path, rec.Code, c.contem)
		}
	}
}

func TestExemplosEndpoints(t *testing.T) {
	// lista
	rec := httptest.NewRecorder()
	handleExemplos(rec, httptest.NewRequest(http.MethodGet, "/exemplos", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /exemplos: %d %s", rec.Code, rec.Body.String())
	}
	var lista []map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &lista); err != nil || len(lista) < 19 {
		t.Fatalf("lista inválida (%d itens): %v", len(lista), err)
	}
	// todo exemplo: o cupom resolve (imagens inclusive) e vira bytes ESC/POS sem erro
	for _, it := range lista {
		rec := httptest.NewRecorder()
		handleExemplos(rec, httptest.NewRequest(http.MethodGet, it["cupom"], nil))
		if rec.Code != 200 {
			t.Fatalf("GET %s: %d %s", it["cupom"], rec.Code, rec.Body.String())
		}
		var c Cupom
		if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
			t.Fatalf("%s: não é um Cupom: %v", it["id"], err)
		}
		if strings.Contains(rec.Body.String(), `"@`) {
			t.Fatalf("%s: referência de imagem (@) não foi resolvida", it["id"])
		}
		titulo := ""
		if c.Titulo != nil {
			titulo = *c.Titulo
		}
		if _, err := montarCupom(titulo, c.Linhas); err != nil {
			t.Fatalf("%s: montarCupom falhou: %v", it["id"], err)
		}
	}
	// id inexistente -> 404 ; método errado -> 405
	rec = httptest.NewRecorder()
	handleExemplos(rec, httptest.NewRequest(http.MethodGet, "/exemplos/nao-existe", nil))
	if rec.Code != 404 {
		t.Fatalf("exemplo inexistente deveria dar 404, veio %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handleExemplos(rec, httptest.NewRequest(http.MethodGet, "/exemplos/teste/print", nil))
	if rec.Code != 405 {
		t.Fatalf("GET em /print deveria dar 405, veio %d", rec.Code)
	}
	// não dá para ler arquivos arbitrários pelo @arquivo
	if _, err := resolverImagem("@../server.go"); err == nil {
		t.Fatal("referência com .. deveria ser recusada")
	}
}

func TestExemplosImprimem(t *testing.T) {
	var captured []byte
	orig := enviar
	enviar = func(dados []byte, cortar bool, feedCorte int) error { captured = dados; return nil }
	defer func() { enviar = orig }()
	if !devicePresent() {
		t.Skip("sem impressora: executarPrint exige o device") // mesmo motivo dos demais testes de /print
	}
	rec := httptest.NewRecorder()
	handleExemplos(rec, httptest.NewRequest(http.MethodPost, "/exemplos/pedido/print", nil))
	if rec.Code != 200 || !bytes.Contains(captured, []byte("PEDIDO #123")) {
		t.Fatalf("POST /exemplos/pedido/print: %d %s", rec.Code, rec.Body.String())
	}
}
