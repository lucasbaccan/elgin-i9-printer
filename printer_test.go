package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMontarCupomInicializacao(t *testing.T) {
	out := montarCupom("", nil)
	want := []byte("\x1b\x40\x1b\x4d\x00") // ESC @ + ESC M 0 (Fonte A)
	if !bytes.HasPrefix(out, want) {
		t.Fatalf("cupom deveria começar com ESC @ + ESC M 0 (Fonte A), veio %x", out[:8])
	}
}

func TestMontarCupomTituloLargura2x(t *testing.T) {
	out := montarCupom("PEDIDO #123", nil)
	// largura 2x liga, título centralizado, largura normal volta
	if !bytes.Contains(out, append(append([]byte{}, largeOn...), alignCenter...)) {
		t.Fatal("título deveria usar GS ! 16 + centralizar")
	}
	if !bytes.Contains(out, append([]byte("PEDIDO #123"), '\n')) {
		t.Fatal("título não encontrado no cupom")
	}
	// largura normal é restaurada após o título
	if !bytes.Contains(out, largeOff) {
		t.Fatal("largura normal (GS ! 0) deveria ser restaurada após o título")
	}
}

func TestMontarCupomTituloTruncado(t *testing.T) {
	longo := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 32 chars > 24
	out := montarCupom(longo, nil)
	// o título não pode exceder 24 colunas
	if bytes.Contains(out, []byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")) {
		t.Fatal("título de 32 chars deveria ser truncado para 24")
	}
	if !bytes.Contains(out, []byte("AAAAAAAAAAAAAAAAAAAAAAAA")) { // 24 A's
		t.Fatal("título truncado de 24 chars não encontrado")
	}
}

func TestMontarCupomLinhaVaziaViraEspaco(t *testing.T) {
	out := montarCupom("", []Linha{{Texto: "", Alinhamento: "centro"}})
	// a linha vazia deve virar um espaço invisível + \n (a i9 não avança linha 100% vazia)
	if !bytes.Contains(out, []byte(" \n")) {
		t.Fatalf("linha vazia deveria virar ' ' + \\n, saída: %q", out)
	}
}

func TestMontarCupomPadraoPreenche48(t *testing.T) {
	out := montarCupom("", []Linha{{Texto: "-X-", Alinhamento: "esquerda", Padrao: true}})
	filled := preencher("-X-", WidthNormal)
	if len([]rune(filled)) != WidthNormal {
		t.Fatalf("preencher deveria gerar %d colunas, gerou %d", WidthNormal, len([]rune(filled)))
	}
	if !bytes.Contains(out, []byte(filled)) {
		t.Fatal("linha com padrao não foi preenchida corretamente")
	}
}

func TestMontarCupomNegrito(t *testing.T) {
	out := montarCupom("", []Linha{{Texto: "NEGRITO", Alinhamento: "centro", Negrito: true}})
	// Ordem real (igual ao Python): negrito liga -> alinhamento -> texto.
	seq := append(append(append([]byte{}, boldOn...), alignCenter...), []byte("NEGRITO")...)
	if !bytes.Contains(out, seq) {
		t.Fatalf("negrito deveria ligar antes do alinhamento+texto; saída: %q", out)
	}
	if !bytes.Contains(out, boldOff) {
		t.Fatal("negrito deveria desligar após o texto")
	}
}

func TestMontarCupomAlinhamentos(t *testing.T) {
	cases := map[string][]byte{
		"esquerda": alignLeft,
		"centro":   alignCenter,
		"direita":  alignRight,
		"??":       alignCenter, // valor inválido cai em centro (padrão)
	}
	for al, cmd := range cases {
		out := montarCupom("", []Linha{{Texto: "x", Alinhamento: al}})
		if !bytes.Contains(out, append(append([]byte{}, cmd...), 'x')) {
			t.Fatalf("alinhamento %q deveria usar %x antes do texto", al, cmd)
		}
	}
}

func TestMontarCupomMoldura48(t *testing.T) {
	out := montarCupom("", nil)
	moldura := bytes.Repeat([]byte("="), WidthNormal)
	// moldura de abertura (após ESC @ + Fonte A + alinhamento esquerda)
	if bytes.Count(out, moldura) < 2 {
		t.Fatal("cupom deveria ter moldura de abertura e fechamento (48 '=')")
	}
}

func TestMontarCupomNaoContemCorte(t *testing.T) {
	out := montarCupom("T", []Linha{{Texto: "x"}})
	// o corte NUNCA vai no buffer do cupom — vai num write separado (enviar)
	if bytes.Contains(out, cutCmd) {
		t.Fatal("montarCupom não deve conter o comando de corte (GS V 66 0)")
	}
}

func TestPreencher(t *testing.T) {
	if got := preencher("-X", 48); len([]rune(got)) != 48 {
		t.Fatalf("preencher(-X, 48) deveria ter 48 runas, tem %d", len([]rune(got)))
	}
	if got := preencher("", 48); got != "" {
		t.Fatalf("preencher('', 48) deveria ser vazio, veio %q", got)
	}
}

func TestFeedBytes(t *testing.T) {
	if got := feedBytes(5); !bytes.Equal(got, []byte("\x1b\x64\x05")) {
		t.Fatalf("feedBytes(5) = %x, esperado 1b 64 05", got)
	}
	if got := feedBytes(0); !bytes.Equal(got, []byte("\x1b\x64\x01")) {
		t.Fatalf("feedBytes(0) deveria clamar para 1, veio %x", got)
	}
	if got := feedBytes(300); !bytes.Equal(got, []byte("\x1b\x64\xff")) {
		t.Fatalf("feedBytes(300) deveria clamar para 255, veio %x", got)
	}
}

func TestCutDelay(t *testing.T) {
	if d := cutDelayBytes(1); d != 1500*time.Millisecond {
		t.Fatalf("cutDelayBytes(1) = %v, esperado 1.5s (mínimo)", d)
	}
	if d := cutDelayBytes(10); d != 2*time.Second {
		t.Fatalf("cutDelayBytes(10) = %v, esperado 2s (10*0.2)", d)
	}
	if d := cutDelayBytes(100); d != 20*time.Second {
		t.Fatalf("cutDelayBytes(100) = %v, esperado 20s", d)
	}
}

func TestEnviarCorteEmWriteSeparado(t *testing.T) {
	// Simula o device com um arquivo temporário.
	f := filepath.Join(t.TempDir(), "lp0")
	if err := os.WriteFile(f, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	origLP := LP
	LP = f
	defer func() { LP = origLP }()

	dados := montarCupom("T", []Linha{{Texto: "conteudo"}})
	if err := enviar(dados, true); err != nil {
		t.Fatalf("enviar falhou: %v", err)
	}

	raw, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	// o corte deve aparecer UMA vez, no final, e após todo o conteúdo.
	if !bytes.HasSuffix(raw, cutCmd) {
		t.Fatal("corte deveria estar no final do device")
	}
	if n := bytes.Count(raw, cutCmd); n != 1 {
		t.Fatalf("corte deveria aparecer 1x, apareceu %d", n)
	}
}

func TestEnviarSemCorte(t *testing.T) {
	f := filepath.Join(t.TempDir(), "lp0")
	if err := os.WriteFile(f, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	origLP := LP
	LP = f
	defer func() { LP = origLP }()

	if err := enviar(feedBytes(3), false); err != nil {
		t.Fatalf("enviar falhou: %v", err)
	}
	raw, _ := os.ReadFile(f)
	if bytes.Contains(raw, cutCmd) {
		t.Fatal("enviar com cortar=false não deveria escrever o corte")
	}
}
