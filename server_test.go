package main

import (
	"bytes"
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
	if !bytes.Contains(captured, []byte("pong")) {
		t.Fatalf("ping deveria imprimir 'pong', bytes enviados: %q", captured)
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

func TestRootPathDesconhecido404(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nao-existe", nil)
	handleRoot(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("path desconhecido deveria retornar 404, veio %d", rec.Code)
	}
}
