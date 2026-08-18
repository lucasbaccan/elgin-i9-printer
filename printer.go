package main

import (
	"bytes"
	"fmt"
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
	codepageCmd = []byte("\x1b\x74\x03") // ESC t 3      ativa a tabela com acentos/símbolos PT (PC860); ESC t 2 é IGNORADO pela i9 (fica em CP437/850: ã→ä, õ→ö, √→¹)
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

// cp860Map converte runas Unicode para os bytes da code page PC860
// (portuguesa) da i9. A faixa 0x00-0x1F é tratada pela impressora como
// comando — glifos como ♥♦♣♠☺ não são acessíveis; os utilizáveis ficam na
// faixa 0x80-0xFF (acentos, box drawing, blocos, letras gregas, símbolos).
// Mapa gerado do codec cp860 do Python (byte -> glifo).
var cp860Map = map[rune]byte{
	'Ç': 0x80, 'ü': 0x81, 'é': 0x82, 'â': 0x83, 'ã': 0x84, 'à': 0x85, 'Á': 0x86, 'ç': 0x87,
	'ê': 0x88, 'Ê': 0x89, 'è': 0x8A, 'Í': 0x8B, 'Ô': 0x8C, 'ì': 0x8D, 'Ã': 0x8E, 'Â': 0x8F,
	'É': 0x90, 'À': 0x91, 'È': 0x92, 'ô': 0x93, 'õ': 0x94, 'ò': 0x95, 'Ú': 0x96, 'ù': 0x97,
	'Ì': 0x98, 'Õ': 0x99, 'Ü': 0x9A, '¢': 0x9B, '£': 0x9C, 'Ù': 0x9D, '₧': 0x9E, 'Ó': 0x9F,
	'á': 0xA0, 'í': 0xA1, 'ó': 0xA2, 'ú': 0xA3, 'ñ': 0xA4, 'Ñ': 0xA5, 'ª': 0xA6, 'º': 0xA7,
	'¿': 0xA8, 'Ò': 0xA9, '¬': 0xAA, '½': 0xAB, '¼': 0xAC, '¡': 0xAD, '«': 0xAE, '»': 0xAF,
	'░': 0xB0, '▒': 0xB1, '▓': 0xB2, '│': 0xB3, '┤': 0xB4, '╡': 0xB5, '╢': 0xB6, '╖': 0xB7,
	'╕': 0xB8, '╣': 0xB9, '║': 0xBA, '╗': 0xBB, '╝': 0xBC, '╜': 0xBD, '╛': 0xBE, '┐': 0xBF,
	'└': 0xC0, '┴': 0xC1, '┬': 0xC2, '├': 0xC3, '─': 0xC4, '┼': 0xC5, '╞': 0xC6, '╟': 0xC7,
	'╚': 0xC8, '╔': 0xC9, '╩': 0xCA, '╦': 0xCB, '╠': 0xCC, '═': 0xCD, '╬': 0xCE, '╧': 0xCF,
	'╨': 0xD0, '╤': 0xD1, '╥': 0xD2, '╙': 0xD3, '╘': 0xD4, '╒': 0xD5, '╓': 0xD6, '╫': 0xD7,
	'╪': 0xD8, '┘': 0xD9, '┌': 0xDA, '█': 0xDB, '▄': 0xDC, '▌': 0xDD, '▐': 0xDE, '▀': 0xDF,
	'α': 0xE0, 'ß': 0xE1, 'Γ': 0xE2, 'π': 0xE3, 'Σ': 0xE4, 'σ': 0xE5, 'µ': 0xE6, 'τ': 0xE7,
	'Φ': 0xE8, 'Θ': 0xE9, 'Ω': 0xEA, 'δ': 0xEB, '∞': 0xEC, 'φ': 0xED, 'ε': 0xEE, '∩': 0xEF,
	'≡': 0xF0, '±': 0xF1, '≥': 0xF2, '≤': 0xF3, '⌠': 0xF4, '⌡': 0xF5, '÷': 0xF6, '≈': 0xF7,
	'°': 0xF8, '∙': 0xF9, '·': 0xFA, '•': 0xFA, '√': 0xFB, 'ⁿ': 0xFC, '²': 0xFD, '■': 0xFE,
}

// encodeTexto converte a string UTF-8 para os bytes da code page PC860.
// Caracteres não mapeados viram '?' (evita lixo de UTF-8 cru na impressora).
func encodeTexto(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 0x80 {
			out = append(out, byte(r)) // ASCII direto
			continue
		}
		if b, ok := cp860Map[r]; ok {
			out = append(out, b)
		} else {
			out = append(out, '?')
		}
	}
	return out
}

// Linha representa uma linha do cupom. JSON compatível com a API antiga
// (FastAPI): texto, alinhamento, fonte, negrito, linha.
type Linha struct {
	Texto       string `json:"texto"`
	Alinhamento string `json:"alinhamento"` // esquerda | centro | direita
	Fonte       string `json:"fonte"`       // normal | larga
	Negrito     bool   `json:"negrito"`
	Linha       bool   `json:"linha"` // true = repete o texto até preencher a linha (antigo "padrao")
}

// Cupom é o payload de POST /print (compatível com a API antiga).
// Não há "moldura" automática: quem quiser moldura adiciona uma linha com
// texto "=" (ou outro) e linha=true — a moldura é só uma linha repetida.
// compact (bool): true/ausente = corte rente à última linha (modo compacto,
// padrão); false = respiro de feedCortePadrao linhas antes do corte.
type Cupom struct {
	Titulo *string `json:"titulo"`
	Linhas []Linha `json:"linhas"`
	Compact *bool  `json:"compact"`
}

// feedCortePadrao é o respiro (linhas em branco) antes do corte quando
// compact=false.
const feedCortePadrao = 3

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

// wrapTexto quebra o texto em pedaços de até `limite` runas — cada pedaço
// vira uma linha. Textos maiores que a largura da linha (48 normal / 24
// larga) viram várias linhas em vez de serem truncados.
func wrapTexto(texto string, limite int) []string {
	r := []rune(texto)
	if len(r) <= limite {
		return []string{texto}
	}
	var out []string
	for len(r) > 0 {
		n := limite
		if len(r) < n {
			n = len(r)
		}
		out = append(out, string(r[:n]))
		r = r[n:]
	}
	return out
}

// montarCupom gera os bytes ESC/POS do cupom SEM o corte. O corte vai num
// write separado (ver enviar) porque a i9 o executa imediatamente ao receber.
// Sem moldura automática: o cupom é título + linhas; moldura = linha "="
// com linha=true (é só uma linha repetida).
func montarCupom(titulo string, linhas []Linha) []byte {
	var buf bytes.Buffer
	buf.Write(initCmd)
	buf.Write(codepageCmd) // PC860 (portuguesa): acentos + símbolos da faixa alta
	buf.Write(fontA)
	buf.Write(alignLeft)

	if titulo != "" {
		buf.Write(largeOn)
		buf.Write(alignCenter)
		// título também quebra em várias linhas se passar de 24 colunas
		for _, p := range wrapTexto(titulo, WidthLarge) {
			buf.Write(encodeTexto(p))
			buf.WriteByte('\n')
		}
		buf.Write(largeOff)
	}

	for _, l := range linhas {
		larga := l.Fonte == "larga"
		limite := WidthNormal
		if larga {
			limite = WidthLarge
		}
		texto := l.Texto
		if l.Linha {
			texto = preencher(l.Texto, limite)
		}
		pedacos := wrapTexto(texto, limite)
		for _, p := range pedacos {
			if strings.TrimSpace(p) == "" {
				// a i9 NÃO avança papel em linha 100% vazia (feed colapsa):
				// manda um espaço invisível para forçar a impressão/avanço.
				p = " "
			}
			if larga {
				buf.Write(largeOn)
			}
			if l.Negrito {
				buf.Write(boldOn)
			}
			buf.Write(alignFor(l.Alinhamento))
			buf.Write(encodeTexto(p))
			buf.WriteByte('\n')
			if l.Negrito {
				buf.Write(boldOff)
			}
			if larga {
				buf.Write(largeOff)
			}
		}
	}

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
	titulo := "CUPOM DE TESTE"
	// moldura = linha "=" com linha=true (não há moldura automática)
	return montarCupom(titulo, []Linha{
		{Texto: "=", Alinhamento: "esquerda", Linha: true},
		{Texto: "Acentos: ç ã é ô õ à", Alinhamento: "esquerda"},
		{Texto: "Símbolos: √ ≈ ≤ ≥ ∞ Ω π µ", Alinhamento: "esquerda"},
		{Texto: ""},
		{Texto: "Texto à esquerda", Alinhamento: "esquerda"},
		{Texto: "Texto centralizado", Alinhamento: "centro"},
		{Texto: "Texto à direita", Alinhamento: "direita"},
		{Texto: ""},
		{Texto: "Fonte larga + negrito", Alinhamento: "centro", Fonte: "larga", Negrito: true},
		{Texto: "=", Alinhamento: "esquerda", Linha: true},
	})
}

// lineDelay é o intervalo entre linhas ao enviar. A i9 tem buffer interno
// pequeno: um write único grande enche o buffer e a impressora para e retoma
// (micro-stalls visíveis). Enviando linha a linha com um pequeno delay — como
// o script bash original fazia — o fluxo de impressão fica contínuo.
// Ajustável via ELGIN_LINE_DELAY_MS (default 30ms).
var lineDelay = 30 * time.Millisecond

// enviar escreve o cupom no device e (opcionalmente) dispara o corte num write
// SEPARADO. A i9 executa o GS V imediatamente ao receber, furando o buffer de
// impressão — por isso o corte não vai junto com o conteúdo.
//
// feedCorte > 0: avança N linhas (ESC d n) ANTES do corte — respiro entre a
// última linha e a guilhotina, sem precisar de linhas em branco no texto
// (0 = corte rente, modo compacto).
//
// É uma var para permitir sobrescrever em testes.
var enviar = func(dados []byte, cortar bool, feedCorte int) error {
	// abrirSaida é por plataforma: device usblp (Linux) ou fila RAW (Windows).
	out, err := abrirSaida()
	if err != nil {
		return err
	}
	defer out.Close()
	// Linha a linha (não um write único): o buffer da i9 é pequeno e rajadas
	// grandes causam "imprime um pedaço, para, continua". Cada linha é um
	// write pequeno; o sleep suaviza o envio para a velocidade da impressora.
	chunks := bytes.Split(dados, []byte{'\n'})
	for i, chunk := range chunks {
		if _, err := out.Write(chunk); err != nil {
			return fmt.Errorf("escrevendo em %s: %w", LP, err)
		}
		if i < len(chunks)-1 {
			if _, err := out.Write([]byte{'\n'}); err != nil {
				return fmt.Errorf("escrevendo em %s: %w", LP, err)
			}
			time.Sleep(lineDelay)
		}
	}
	if cortar {
		// GS V 66 0 = "print and feed to cutting position and cut": a i9
		// termina de imprimir o buffer e só então corta rente. MAS o firmware
		// executa o GS V imediatamente ao receber — se a última linha ainda
		// está no pipeline USB, ela pode sair duplicada/depois do corte. Um
		// delay curto (250ms) garante que a última linha terminou de imprimir.
		time.Sleep(250 * time.Millisecond)
		if feedCorte > 0 {
			if _, err := out.Write(feedBytes(feedCorte)); err != nil {
				return fmt.Errorf("escrevendo feed pré-corte em %s: %w", LP, err)
			}
		}
		if _, err := out.Write(cutCmd); err != nil {
			return fmt.Errorf("escrevendo corte em %s: %w", LP, err)
		}
	}
	return nil
}
