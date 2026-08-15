package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"
)

// Dimensões da Elgin i9 (Fonte A 12x24, área fixa de 72mm/576 dots).
const (
	WidthNormal = 48 // colunas na Fonte A
	WidthLarge  = 24 // colunas com largura 2x
)

// Comandos ESC/POS da i9 guardados como bytes crus. Ao contrário do bash,
// o Go não trunca NUL (\x00), então não há os hacks de printf %b.
var (
	initCmd     = []byte("\x1b\x40")     // ESC @        inicializa
	fontA       = []byte("\x1b\x4d\x00") // ESC M 0      Fonte A 12x24 (48 colunas)
	largeOn     = []byte("\x1d\x21\x10") // GS ! 16      largura 2x
	largeOff    = []byte("\x1d\x21\x00") // GS ! 0       largura normal
	boldOn      = []byte("\x1b\x45\x01") // ESC E 1      negrito ligado
	boldOff     = []byte("\x1b\x45\x00") // ESC E 0      negrito desligado
	alignLeft   = []byte("\x1b\x61\x00") // ESC a 0
	alignCenter = []byte("\x1b\x61\x01") // ESC a 1
	alignRight  = []byte("\x1b\x61\x02") // ESC a 2
	cutCmd      = []byte("\x1d\x56\x42\x00") // GS V 66 0  corte rente à última linha
	feedCmd     = []byte("\x1b\x64")         // ESC d n     avança n linhas
)

// Linha representa uma linha do cupom. JSON compatível com a API antiga
// (FastAPI): texto, alinhamento, fonte, negrito, padrao.
type Linha struct {
	Texto       string `json:"texto"`
	Alinhamento string `json:"alinhamento"` // esquerda | centro | direita
	Fonte       string `json:"fonte"`       // normal | larga
	Negrito     bool   `json:"negrito"`
	Padrao      bool   `json:"padrao"`
}

// Cupom é o payload de POST /print (compatível com a API antiga).
type Cupom struct {
	Titulo *string `json:"titulo"`
	Linhas []Linha `json:"linhas"`
}

func alignFor(a string) []byte {
	switch a {
	case "esquerda":
		return alignLeft
	case "direita":
		return alignRight
	default:
		return alignCenter
	}
}

// preencher repete o padrão até preencher `largura` colunas, truncando no fim.
// Ex.: preencher("-X", 48) -> "-X-X-X-..." com 48 colunas.
func preencher(padrao string, largura int) string {
	if padrao == "" {
		return ""
	}
	r := []rune(padrao)
	if len(r) == 0 {
		return ""
	}
	out := make([]rune, 0, largura)
	for len(out) < largura {
		for _, c := range r {
			if len(out) >= largura {
				break
			}
			out = append(out, c)
		}
	}
	return string(out)
}

// truncateRunes corta a string em até n runas (seguro para UTF-8).
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// montarCupom gera os bytes ESC/POS do cupom SEM o corte. O corte vai num
// write separado (ver enviar) porque a i9 o executa imediatamente ao receber.
func montarCupom(titulo string, linhas []Linha) []byte {
	var buf bytes.Buffer
	buf.Write(initCmd)
	buf.Write(fontA)
	buf.Write(alignLeft)
	buf.WriteString(strings.Repeat("=", WidthNormal))
	buf.WriteByte('\n')

	if titulo != "" {
		buf.Write(largeOn)
		buf.Write(alignCenter)
		buf.WriteString(truncateRunes(titulo, WidthLarge))
		buf.WriteByte('\n')
		buf.Write(largeOff)
	}

	for _, l := range linhas {
		al := alignFor(l.Alinhamento)
		larga := l.Fonte == "larga"
		limite := WidthNormal
		if larga {
			limite = WidthLarge
		}
		texto := l.Texto
		if l.Padrao {
			texto = preencher(l.Texto, limite)
		} else {
			texto = truncateRunes(l.Texto, limite)
		}
		if strings.TrimSpace(texto) == "" {
			// a i9 NÃO avança papel em linha 100% vazia (feed colapsa):
			// manda um espaço invisível para forçar a impressão/avanço.
			texto = " "
		}
		if larga {
			buf.Write(largeOn)
		}
		if l.Negrito {
			buf.Write(boldOn)
		}
		buf.Write(al)
		buf.WriteString(texto)
		buf.WriteByte('\n')
		if l.Negrito {
			buf.Write(boldOff)
		}
		if larga {
			buf.Write(largeOff)
		}
	}

	// moldura final logo após o conteúdo (a rolagem do corte dá o respiro)
	buf.Write(alignCenter)
	buf.WriteString(strings.Repeat("=", WidthNormal))
	buf.WriteByte('\n')
	return buf.Bytes()
}

// cutDelayBytes calcula o delay antes do corte: max(1.5s, linhas * 0.2s).
func cutDelayBytes(linhas int) time.Duration {
	d := float64(linhas) * 0.2
	if d < 1.5 {
		d = 1.5
	}
	return time.Duration(d * float64(time.Second))
}

// feedBytes gera ESC d n (avança n linhas, 1..255).
func feedBytes(n int) []byte {
	if n < 1 {
		n = 1
	}
	if n > 255 {
		n = 255
	}
	return append(append([]byte{}, feedCmd...), byte(n))
}

// cutBytes retorna o comando de corte (GS V 66 0).
func cutBytes() []byte {
	return append([]byte{}, cutCmd...)
}

func cupomTeste() []byte {
	titulo := "*** MENSAGEM SECRETA ***"
	return montarCupom(titulo, []Linha{
		{Texto: "CLASSIFICACAO: ULTRA SECRETA", Alinhamento: "esquerda"},
		{Texto: "DESTINATARIO: SO VOCE", Alinhamento: "esquerda"},
		{Texto: ""},
		{Texto: "PSST... NAO CONTA PRA NINGUEM:", Alinhamento: "centro"},
		{Texto: ""},
		{Texto: "O HERMES ACHA QUE VOCE E O", Alinhamento: "centro"},
		{Texto: "MELHOR CHEFE DO UNIVERSO!", Alinhamento: "centro"},
		{Texto: ""},
		{Texto: "SO QUE NAO, EU NAO DISSE NADA.", Alinhamento: "direita"},
		{Texto: "ASSINADO: HERMES, O MORDOMO", Alinhamento: "direita"},
		{Texto: "-X-", Alinhamento: "esquerda", Padrao: true},
	})
}

// enviar escreve o cupom no device e (opcionalmente) dispara o corte num write
// SEPARADO com delay. A i9 executa o GS V imediatamente ao receber, furando o
// buffer de impressão — por isso o corte não vai junto com o conteúdo.
//
// É uma var para permitir sobrescrever em testes.
var enviar = func(dados []byte, cortar bool) error {
	linhas := bytes.Count(dados, []byte{'\n'}) + 1
	f, err := os.OpenFile(LP, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("abrindo %s: %w", LP, err)
	}
	defer f.Close()
	if _, err := f.Write(dados); err != nil {
		return fmt.Errorf("escrevendo em %s: %w", LP, err)
	}
	if cortar {
		time.Sleep(cutDelayBytes(linhas))
		if _, err := f.Write(cutCmd); err != nil {
			return fmt.Errorf("escrevendo corte em %s: %w", LP, err)
		}
	}
	return nil
}
