package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pngTeste gera um PNG pequeno; cor diferente => conteúdo (e hash) diferente.
func pngTeste(t *testing.T, tom uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = tom
	}
	img.SetGray(1, 1, color.Gray{Y: 255 - tom})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// armazenamentoTemp aponta o armazenamento para um diretório temporário.
func armazenamentoTemp(t *testing.T) {
	t.Helper()
	origDir, origTTL, origMax := dataDir, imgTTL, imgMaxTotal
	dataDir = t.TempDir()
	imgTTL, imgMaxTotal = imgTTLPadrao, imgMaxTotalPadrao
	t.Cleanup(func() { dataDir, imgTTL, imgMaxTotal = origDir, origTTL, origMax })
}

func TestImagemMesmoConteudoMesmoIdERenova(t *testing.T) {
	armazenamentoTemp(t)
	raw := pngTeste(t, 100)
	id1, exp1, renovada1, err := salvarImagem(raw)
	if err != nil || renovada1 {
		t.Fatalf("primeiro envio: id=%q renovada=%v err=%v", id1, renovada1, err)
	}
	if !imgIDValido.MatchString(id1) {
		t.Fatalf("id deveria ter 32 hexadecimais, veio %q", id1)
	}
	// outra imagem => outro id; a mesma de novo => o mesmo id
	if id2, _, _, _ := salvarImagem(pngTeste(t, 30)); id2 == id1 {
		t.Fatal("imagens diferentes não podem ter o mesmo id")
	}
	// envelhece o arquivo em 6 dias e compartilha de novo: renova (volta a 7 dias de validade)
	caminho := filepath.Join(imgDir(), id1+".png")
	velho := time.Now().Add(-6 * 24 * time.Hour)
	os.Chtimes(caminho, velho, velho)
	id3, exp3, renovada3, err := salvarImagem(raw)
	if err != nil || id3 != id1 || !renovada3 {
		t.Fatalf("mesma imagem deveria dar o mesmo id e renovar: id=%q renovada=%v err=%v", id3, renovada3, err)
	}
	if exp3.Sub(exp1) < -time.Minute || time.Until(exp3) < 6*24*time.Hour {
		t.Fatalf("o prazo deveria voltar a ~7 dias: %v", time.Until(exp3))
	}
	if entradas, _ := os.ReadDir(imgDir()); len(entradas) != 2 {
		t.Fatalf("a imagem repetida não pode duplicar arquivo: %d arquivos", len(entradas))
	}
}

func TestImagemExpiradaNaoEServidaELimpezaApaga(t *testing.T) {
	armazenamentoTemp(t)
	id, _, _, _ := salvarImagem(pngTeste(t, 50))
	idVelho, _, _, _ := salvarImagem(pngTeste(t, 90))
	caminhoVelho := filepath.Join(imgDir(), idVelho+".png")
	venceu := time.Now().Add(-8 * 24 * time.Hour)
	os.Chtimes(caminhoVelho, venceu, venceu)
	// sobra de envio interrompido com mais de 1 h também é apagada
	sobra := filepath.Join(imgDir(), ".envio-abc")
	os.WriteFile(sobra, []byte("x"), 0o644)
	os.Chtimes(sobra, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour))

	if _, _, _, err := lerImagem(id); err != nil {
		t.Fatalf("imagem dentro do prazo deveria ser servida: %v", err)
	}
	if n := limparImagensExpiradas(); n != 2 {
		t.Fatalf("a limpeza deveria apagar 2 arquivos (vencida + sobra), apagou %d", n)
	}
	if _, err := os.Stat(caminhoVelho); err == nil {
		t.Fatal("arquivo vencido deveria ter sido apagado")
	}
	if _, _, _, err := lerImagem(id); err != nil {
		t.Fatal("a limpeza não pode apagar o que está no prazo")
	}
	// vencida mas ainda em disco (limpeza não rodou): não é servida
	os.Chtimes(filepath.Join(imgDir(), id+".png"), venceu, venceu)
	if _, _, _, err := lerImagem(id); err == nil {
		t.Fatal("imagem vencida não deveria ser servida")
	}
}

func TestImagemLimitesEIdSeguro(t *testing.T) {
	armazenamentoTemp(t)
	if _, _, _, err := salvarImagem([]byte("não é imagem")); err != errImgInvalida {
		t.Fatalf("lixo deveria dar errImgInvalida, veio %v", err)
	}
	if _, _, _, err := salvarImagem(append([]byte{0x89, 'P', 'N', 'G'}, make([]byte, imgMaxBytes)...)); err != errImgGrande {
		t.Fatalf("imagem enorme deveria dar errImgGrande, veio %v", err)
	}
	// limite total do armazenamento
	imgMaxTotal = 10 // menor que qualquer PNG
	if _, _, _, err := salvarImagem(pngTeste(t, 10)); err != errImgCheio {
		t.Fatalf("passando do limite total deveria dar errImgCheio, veio %v", err)
	}
	// ids que tentam sair da pasta ou não são hash
	for _, id := range []string{"../../etc/passwd", "..%2f..", strings.Repeat("g", 32), "abc", ""} {
		if _, _, _, err := lerImagem(id); err == nil {
			t.Errorf("id %q deveria ser recusado", id)
		}
	}
}

func TestImagensEndpoints(t *testing.T) {
	armazenamentoTemp(t)
	raw := pngTeste(t, 70)
	corpo, _ := json.Marshal(map[string]string{"imagem": "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)})

	envia := func() map[string]any {
		rec := httptest.NewRecorder()
		handleImagens(rec, httptest.NewRequest(http.MethodPost, "/imagens", bytes.NewReader(corpo)))
		if rec.Code != 200 {
			t.Fatalf("POST /imagens: %d %s", rec.Code, rec.Body.String())
		}
		var r map[string]any
		json.Unmarshal(rec.Body.Bytes(), &r)
		return r
	}
	r1 := envia()
	if r1["renovada"] != false || r1["validade_dias"] != float64(7) {
		t.Fatalf("primeiro envio: %v", r1)
	}
	if r2 := envia(); r2["id"] != r1["id"] || r2["renovada"] != true {
		t.Fatalf("segundo envio deveria renovar o mesmo id: %v", r2)
	}
	if _, err := time.Parse(time.RFC3339, r1["expira_em"].(string)); err != nil {
		t.Fatalf("expira_em inválido: %v", err)
	}
	// GET devolve os mesmos bytes, com o tipo certo e nosniff
	rec := httptest.NewRecorder()
	handleImagens(rec, httptest.NewRequest(http.MethodGet, r1["url"].(string), nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" || !bytes.Equal(rec.Body.Bytes(), raw) {
		t.Fatalf("GET /imagens/{id}: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	// inexistente/expirada -> 404 ; lixo -> 400 ; método errado -> 405
	for _, c := range []struct {
		metodo, caminho, corpo string
		esperado               int
	}{
		{"GET", "/imagens/" + strings.Repeat("0", 32), "", 404},
		{"GET", "/imagens/../server.go", "", 404},
		{"POST", "/imagens", `{"imagem":"bm90IGltYWdl"}`, 400},
		{"POST", "/imagens", `nao e json`, 400},
		{"GET", "/imagens", "", 405},
		{"DELETE", "/imagens/" + r1["id"].(string), "", 405},
	} {
		rec := httptest.NewRecorder()
		handleImagens(rec, httptest.NewRequest(c.metodo, c.caminho, strings.NewReader(c.corpo)))
		if rec.Code != c.esperado {
			t.Errorf("%s %s: esperado %d, veio %d", c.metodo, c.caminho, c.esperado, rec.Code)
		}
	}
}
