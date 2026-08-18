package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// Blocos gráficos do cupom: imagem (foto/logo) e QR code. Ambos viram um
// bit image raster ESC/POS (GS v 0), 1-bit, com a largura limitada ao papel
// de 80mm (576 dots @ 203dpi) e escala automática mantendo a proporção.

const (
	dotWidth    = 576 // 80mm @ 203dpi = 576 dots (área de impressão fixa da i9)
	qrModPadrao = 4   // tamanho de módulo padrão do QR (1..8)
	// limite de segurança para evitar OOM com imagens enormes
	maxPixels = 50_000_000
)

// decodeImagem converte base64 (com ou sem o prefixo data:image/...;base64,)
// num image.Image decodificado (PNG, JPEG ou GIF — primeiro frame).
func decodeImagem(s string) (image.Image, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "base64,"); i >= 0 {
		s = s[i+len("base64,"):]
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("imagem: base64 inválido: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("imagem: formato não suportado (use PNG, JPEG ou GIF): %w", err)
	}
	return img, nil
}

// grayScale converte a imagem em matriz de luminância 0..255.
func grayScale(img image.Image) [][]uint8 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([][]uint8, h)
	for y := 0; y < h; y++ {
		row := make([]uint8, w)
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			// RGBA() devolve 16 bits; reduz para 8 e pesa por luminância percebida.
			lum := (299*int(r>>8) + 587*int(g>>8) + 114*int(bl>>8)) / 1000
			row[x] = uint8(lum)
		}
		out[y] = row
	}
	return out
}

// escalaParaLargura reduz a largura para maxWidth (box average) mantendo a
// proporção. Reduzir com média de área evita o aliasing do nearest-neighbor.
func escalaParaLargura(gray [][]uint8, maxWidth int) [][]uint8 {
	h := len(gray)
	w := len(gray[0])
	if w <= maxWidth {
		return gray
	}
	newW := maxWidth
	newH := int(math.Round(float64(h) * float64(maxWidth) / float64(w)))
	if newH < 1 {
		newH = 1
	}
	out := make([][]uint8, newH)
	for y := 0; y < newH; y++ {
		row := make([]uint8, newW)
		y0 := y * h / newH
		y1 := (y + 1) * h / newH
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < newW; x++ {
			x0 := x * w / newW
			x1 := (x + 1) * w / newW
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sum, n int
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					sum += int(gray[yy][xx])
					n++
				}
			}
			row[x] = uint8(sum / n)
		}
		out[y] = row
	}
	return out
}

// ditherFloydSteinberg converte a luminância em bitmap 1-bit (row-major,
// w*h bools, true = preto) com difusão de erro — muito melhor que um
// threshold fixo em logos/fotos com bordas suaves.
func ditherFloydSteinberg(gray [][]uint8) []bool {
	h := len(gray)
	w := len(gray[0])
	buf := make([][]int, h)
	for y := 0; y < h; y++ {
		row := make([]int, w)
		for x := 0; x < w; x++ {
			row[x] = int(gray[y][x])
		}
		buf[y] = row
	}
	bits := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			old := buf[y][x]
			novo := 0
			if old > 127 {
				novo = 255
				bits[y*w+x] = true
			}
			err := old - novo
			if x+1 < w {
				buf[y][x+1] += err * 7 / 16
			}
			if y+1 < h {
				if x > 0 {
					buf[y+1][x-1] += err * 3 / 16
				}
				buf[y+1][x] += err * 5 / 16
				if x+1 < w {
					buf[y+1][x+1] += err * 1 / 16
				}
			}
		}
	}
	return bits
}

// gsv0Header monta o cabeçalho GS v 0 (raster bit image, m=0): xL/xH em
// BYTES (largura/8), yL/yH em DOTS (altura). Largura máx 576 dots = 72 bytes,
// dentro do limite de 1023 bytes do comando.
func gsv0Header(w, h int) []byte {
	widthBytes := (w + 7) / 8
	return []byte{
		0x1d, 0x76, 0x30, 0x00, // GS v 0 m=0 (8-dot single density)
		byte(widthBytes), byte(widthBytes >> 8), // xL xH (bytes horizontais)
		byte(h), byte(h >> 8), // yL yH (dots verticais)
	}
}

// gsv0Data serializa o bitmap row-major em dados GS v 0: cada byte = 8 dots
// HORIZONTAIS (bit 7 = dot mais à esquerda), linha a linha de cima para baixo.
func gsv0Data(bits []bool, w, h int) []byte {
	widthBytes := (w + 7) / 8
	out := make([]byte, 0, widthBytes*h)
	for y := 0; y < h; y++ {
		for bx := 0; bx < widthBytes; bx++ {
			var b byte
			for i := 0; i < 8; i++ {
				x := bx*8 + i
				if x < w && bits[y*w+x] {
					b |= 1 << (7 - i)
				}
			}
			out = append(out, b)
		}
	}
	return out
}

// alinharBits preenche o bitmap com branco até a largura do papel (dotWidth)
// conforme o alinhamento: esquerda não preenche (a impressora já cola à
// margem), centro divide o respiro dos dois lados, direita só à esquerda.
// Devolve os bits (possivelmente alargados) e a nova largura.
func alinharBits(bits []bool, w, h int, alinhamento string) ([]bool, int) {
	if w >= dotWidth || alinhamento == "esquerda" {
		return bits, w
	}
	newW := dotWidth
	offset := newW - w // direita
	if alinhamento != "direita" {
		offset = (newW - w) / 2 // centro (padrão)
	}
	out := make([]bool, newW*h)
	for y := 0; y < h; y++ {
		copy(out[y*newW+offset:], bits[y*w:(y+1)*w])
	}
	return out, newW
}

// imagemParaGSv0 leva uma imagem decodificada ao raster GS v 0 (1-bit,
// largura <= 576 dots, proporção mantida, dither). alinhamento: esquerda |
// centro | direita.
func imagemParaGSv0(img image.Image, alinhamento string) ([]byte, error) {
	b := img.Bounds()
	if b.Dx()*b.Dy() > maxPixels {
		return nil, fmt.Errorf("imagem: muito grande (%dx%d px); reduza antes de enviar", b.Dx(), b.Dy())
	}
	gray := grayScale(img)
	if len(gray[0]) > dotWidth {
		gray = escalaParaLargura(gray, dotWidth)
	}
	h := len(gray)
	w := len(gray[0])
	bits := ditherFloydSteinberg(gray)
	bits, w = alinharBits(bits, w, h, alinhamento)
	return append(gsv0Header(w, h), gsv0Data(bits, w, h)...), nil
}

// qrParaGSv0 gera o QR do conteúdo e o serializa como raster GS v 0 (fallback
// por imagem — funciona em qualquer impressora ESC/POS, sem depender do
// suporte a GS ( k da i9). modSize é o tamanho do módulo (1..8; padrão 4);
// se o QR não couber em 576 dots, o módulo é reduzido automaticamente.
func qrParaGSv0(conteudo string, modSize int, alinhamento string) ([]byte, error) {
	if strings.TrimSpace(conteudo) == "" {
		return nil, errors.New("qr: conteúdo vazio")
	}
	q, err := qrcode.New(conteudo, qrcode.Medium)
	if err != nil {
		return nil, fmt.Errorf("qr: conteúdo inválido: %w", err)
	}
	// Bitmap() já inclui a quiet zone de 4 módulos exigida pelo padrão.
	matriz := q.Bitmap()
	n := len(matriz)

	mod := modSize
	if mod < 1 {
		mod = qrModPadrao
	}
	if mod > 8 {
		mod = 8
	}
	// cabe no papel? se não, reduz o módulo (mantendo os módulos uniformes —
	// reduzir a imagem inteira quebraria a proporção e a legibilidade).
	if n*mod > dotWidth {
		mod = dotWidth / n
		if mod < 1 {
			mod = 1
		}
	}

	w := n * mod
	h := w
	bits := make([]bool, w*h)
	for my := 0; my < n; my++ {
		for mx := 0; mx < n; mx++ {
			if !matriz[my][mx] {
				continue
			}
			for dy := 0; dy < mod; dy++ {
				for dx := 0; dx < mod; dx++ {
					yy := my*mod + dy
					xx := mx*mod + dx
					bits[yy*w+xx] = true
				}
			}
		}
	}
	bits, w = alinharBits(bits, w, h, alinhamento)
	return append(gsv0Header(w, h), gsv0Data(bits, w, h)...), nil
}

// qrPNG renderiza o QR como PNG (cada módulo com `modSize` pixels) usando o
// MESMO gerador da impressão — o preview da Web UI bate com o que sai no papel.
func qrPNG(conteudo string, modSize int) ([]byte, error) {
	q, err := qrcode.New(conteudo, qrcode.Medium)
	if err != nil {
		return nil, err
	}
	if modSize < 1 {
		modSize = qrModPadrao
	}
	if modSize > 8 {
		modSize = 8
	}
	return q.PNG(len(q.Bitmap()) * modSize)
}
