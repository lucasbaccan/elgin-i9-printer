package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
)

// Exemplos de cupom. A fonte ÚNICA é webui/exemplos/exemplos.json (embutido): a
// Web UI monta o menu "Exemplos" a partir dele e a API expõe cada exemplo:
//
//	GET  /exemplos              lista (id, grupo, nome, descrição, URLs)
//	GET  /exemplos/{id}         o cupom pronto (corpo do POST /print)
//	POST /exemplos/{id}/print   imprime o exemplo
//
// Imagens são referenciadas no JSON por "@arquivo.png" (arquivo em webui/exemplos/)
// ou "@test-grayscale" (o degradê de 16 tons gerado em código) e resolvidas aqui
// para data URL base64.

// Exemplo é uma entrada de exemplos.json. Cupom fica como JSON cru: além dos
// campos do POST /print pode trazer dicas da Web UI (qr_modo, qr_campos) que a
// API ignora.
type Exemplo struct {
	ID        string          `json:"id"`
	Grupo     string          `json:"grupo"`
	Nome      string          `json:"nome"`
	Descricao string          `json:"descricao"`
	Cupom     json.RawMessage `json:"cupom"`
}

var (
	exemplosOnce  sync.Once
	exemplosLista []Exemplo
	exemplosErr   error
)

// carregarExemplos lê (uma vez) o exemplos.json embutido.
func carregarExemplos() ([]Exemplo, error) {
	exemplosOnce.Do(func() {
		raw, err := exemplosFS.ReadFile("webui/exemplos/exemplos.json")
		if err != nil {
			exemplosErr = err
			return
		}
		exemplosErr = json.Unmarshal(raw, &exemplosLista)
	})
	return exemplosLista, exemplosErr
}

func buscarExemplo(id string) (Exemplo, bool) {
	lista, err := carregarExemplos()
	if err != nil {
		return Exemplo{}, false
	}
	for _, e := range lista {
		if e.ID == id {
			return e, true
		}
	}
	return Exemplo{}, false
}

// resolverImagem troca "@arquivo" / "@test-grayscale" pelo data URL da imagem.
func resolverImagem(ref string) (string, error) {
	nome := strings.TrimPrefix(ref, "@")
	if nome == "test-grayscale" {
		png, err := gerarTesteDegradé()
		if err != nil {
			return "", err
		}
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
	}
	if nome != path.Base(nome) || nome == "." || nome == "" { // sem subpastas nem ..
		return "", fmt.Errorf("referência de imagem inválida: %q", ref)
	}
	raw, err := exemplosFS.ReadFile("webui/exemplos/" + nome)
	if err != nil {
		return "", fmt.Errorf("imagem de exemplo %q: %w", nome, err)
	}
	mime := "image/png"
	if strings.HasSuffix(nome, ".jpg") || strings.HasSuffix(nome, ".jpeg") {
		mime = "image/jpeg"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

// cupomDoExemplo devolve o cupom do exemplo com as imagens já resolvidas.
func cupomDoExemplo(e Exemplo) (map[string]any, error) {
	var cupom map[string]any
	if err := json.Unmarshal(e.Cupom, &cupom); err != nil {
		return nil, err
	}
	linhas, _ := cupom["linhas"].([]any)
	for _, l := range linhas {
		m, ok := l.(map[string]any)
		if !ok {
			continue
		}
		if ref, ok := m["imagem"].(string); ok && strings.HasPrefix(ref, "@") {
			img, err := resolverImagem(ref)
			if err != nil {
				return nil, err
			}
			m["imagem"] = img
		}
	}
	return cupom, nil
}

// handleExemplos atende GET /exemplos (lista), GET /exemplos/{id} (cupom) e
// POST /exemplos/{id}/print (imprime).
func handleExemplos(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/exemplos"), "/")
	if rel == "" {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "use GET")
			return
		}
		lista, err := carregarExemplos()
		if err != nil {
			writeErr(w, 500, fmt.Sprintf("exemplos.json inválido: %v", err))
			return
		}
		itens := make([]map[string]string, 0, len(lista))
		for _, e := range lista {
			itens = append(itens, map[string]string{
				"id": e.ID, "grupo": e.Grupo, "nome": e.Nome, "descricao": e.Descricao,
				"cupom": "/exemplos/" + e.ID, "imprimir": "/exemplos/" + e.ID + "/print",
			})
		}
		writeJSON(w, 200, itens)
		return
	}
	id, acao, _ := strings.Cut(rel, "/")
	e, ok := buscarExemplo(id)
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("exemplo %q não encontrado (veja GET /exemplos)", id))
		return
	}
	cupom, err := cupomDoExemplo(e)
	if err != nil {
		writeErr(w, 500, fmt.Sprintf("erro ao montar o exemplo: %v", err))
		return
	}
	switch acao {
	case "":
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "use GET (para imprimir: POST /exemplos/"+id+"/print)")
			return
		}
		writeJSON(w, 200, cupom)
	case "print":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "use POST")
			return
		}
		raw, err := json.Marshal(cupom)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		var c Cupom
		if err := json.Unmarshal(raw, &c); err != nil {
			writeErr(w, 500, fmt.Sprintf("exemplo inválido: %v", err))
			return
		}
		executarPrint(w, c)
	default:
		writeErr(w, http.StatusNotFound, "rota desconhecida")
	}
}
