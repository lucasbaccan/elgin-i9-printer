package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"math"
	"strings"

	"github.com/disintegration/imaging"
	qrcode "github.com/skip2/go-qrcode"
)

// Blocos gráficos do cupom: imagem (foto/logo) e QR code. Ambos viram um
// bit image raster ESC/POS (GS v 0), 1-bit, com a largura limitada ao papel
// de 80mm (576 dots @ 203dpi) e escala automática mantendo a proporção.

const (
	dotWidth    = 576 // 80mm @ 203dpi = 576 dots (área de impressão fixa da i9)
	qrModPadrao = 14  // tamanho de módulo padrão do QR (25 módulos × 14 = 350 dots, ~60% do papel)
	qrModMin    = 3   // módulo mínimo: menores não são legíveis (valores abaixo são ajustados, sem erro)
	qrModMax    = 23  // módulo máximo: 23 × 25 módulos = 575 dots, a largura do papel (acima é ajustado, sem erro)
	// espaço em branco abaixo do QR (1 linha de texto = 24 dots): sem a quiet zone,
	// o QR terminaria rente à linha de corte ou ao texto seguinte
	qrMargemInferior = 24
	// limite de segurança para evitar OOM com imagens enormes
	maxPixels = 50_000_000
)

// decodeImagem converte base64 (com ou sem o prefixo data:image/...;base64,)
// num image.Image decodificado (PNG, JPEG ou GIF — primeiro frame).
func decodeImagem(s string) (image.Image, error) {
	raw, err := base64Imagem(s)
	if err != nil {
		return nil, err
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

// AjustesImagem controla o processamento de uma imagem antes do dither. O zero
// de cada campo é o comportamento padrão — exceto Nitidez e AutoContraste, que
// usam ponteiro (nil = padrão: nitidez 30 e auto-contraste ligado).
type AjustesImagem struct {
	Largura       int   // largura impressa em dots (0 = automática: original, até 576)
	Brilho        int   // -100..100
	Contraste     int   // -100..100
	MeiosTons     int   // -100..100 (+ clareia, − escurece os meios-tons; 0 = gama 1,15)
	Nitidez       *int  // 0..100 (padrão 30)
	AutoContraste *bool // estica o histograma 1%..99% (padrão true)
	Inverter      bool  // negativo
}

const nitidezPadrao = 30

// ajustesDe lê os campos de processamento de um bloco de imagem.
func ajustesDe(l Linha) AjustesImagem {
	return AjustesImagem{
		Largura: l.Largura, Brilho: l.Brilho, Contraste: l.Contraste, MeiosTons: l.MeiosTons,
		Nitidez: l.Nitidez, AutoContraste: l.AutoContraste, Inverter: l.Inverter,
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// prepararImagem leva a imagem a tons de cinza prontos para o dither:
// achata a transparência sobre branco (senão PNG transparente vira preto),
// redimensiona com Lanczos (largura pedida ou original, no máx. 576 dots),
// aplica sharpen, auto-contraste, brilho/contraste/meios-tons e inversão.
func prepararImagem(img image.Image, aj AjustesImagem) [][]uint8 {
	branco := imaging.New(img.Bounds().Dx(), img.Bounds().Dy(), color.White)
	base := imaging.Overlay(branco, img, image.Point{}, 1)
	alvo := base.Bounds().Dx()
	if aj.Largura > 0 {
		alvo = clampInt(aj.Largura, 8, dotWidth)
	} else if alvo > dotWidth {
		alvo = dotWidth
	}
	if alvo != base.Bounds().Dx() {
		base = imaging.Resize(base, alvo, 0, imaging.Lanczos)
	}
	cinza := imaging.Grayscale(base)
	nit := nitidezPadrao
	if aj.Nitidez != nil {
		nit = clampInt(*aj.Nitidez, 0, 100)
	}
	if nit > 0 {
		cinza = imaging.Sharpen(cinza, float64(nit)/50)
	}
	gray := make([][]uint8, cinza.Bounds().Dy())
	for y := range gray {
		gray[y] = make([]uint8, cinza.Bounds().Dx())
		for x := range gray[y] {
			gray[y][x] = cinza.Pix[y*cinza.Stride+x*4]
		}
	}
	if aj.AutoContraste == nil || *aj.AutoContraste {
		esticaContraste(gray)
	}
	ajustaTons(gray, aj)
	return gray
}

// ajustaTons aplica brilho, contraste (fórmula clássica de fator), meios-tons
// (gama = 1,15 · 2^(MeiosTons/100); gama>1 clareia) e a inversão opcional.
func ajustaTons(gray [][]uint8, aj AjustesImagem) {
	brilho := float64(clampInt(aj.Brilho, -100, 100)) * 2.55
	c := float64(clampInt(aj.Contraste, -100, 100)) * 2.55
	fator := (259 * (c + 255)) / (255 * (259 - c))
	gama := 1.15 * math.Pow(2, float64(clampInt(aj.MeiosTons, -100, 100))/100)
	for y := range gray {
		for x, v := range gray[y] {
			f := float64(v)
			f = fator*(f-128) + 128 + brilho
			f = math.Max(0, math.Min(255, f))
			f = math.Pow(f/255, 1/gama) * 255
			if aj.Inverter {
				f = 255 - f
			}
			gray[y][x] = uint8(math.Max(0, math.Min(255, f+0.5)))
		}
	}
}

// esticaContraste faz o auto-contraste: leva o percentil 1% a 0 e o 99% a 255.
// Imagens quase uniformes (faixa < 32) ficam como estão.
func esticaContraste(gray [][]uint8) {
	var hist [256]int
	total := 0
	for _, row := range gray {
		for _, v := range row {
			hist[v]++
			total++
		}
	}
	lo, hi, acc := 0, 255, 0
	for i := 0; i < 256; i++ {
		acc += hist[i]
		if acc*100 >= total {
			lo = i
			break
		}
	}
	acc = 0
	for i := 255; i >= 0; i-- {
		acc += hist[i]
		if acc*100 >= total {
			hi = i
			break
		}
	}
	if hi-lo < 32 {
		return
	}
	for y := range gray {
		for x, v := range gray[y] {
			f := float64(int(v)-lo) / float64(hi-lo)
			gray[y][x] = uint8(math.Max(0, math.Min(1, f))*255 + 0.5)
		}
	}
}

// ditherAtkinson difunde só 6/8 do erro (os outros 2/8 são descartados):
// áreas claras e escuras ficam limpas, com bem menos "ruído" que Floyd-Steinberg
// em fotos — o estilo clássico dos Macs antigos, bom para térmicas. true = preto.
func ditherAtkinson(gray [][]uint8) []bool {
	h := len(gray)
	w := len(gray[0])
	buf := make([][]int, h)
	for y := range buf {
		buf[y] = make([]int, w)
		for x := range buf[y] {
			buf[y][x] = int(gray[y][x])
		}
	}
	bits := make([]bool, w*h)
	add := func(x, y, v int) {
		if x >= 0 && x < w && y < h {
			buf[y][x] += v
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			old := buf[y][x]
			novo := 255
			if old <= 127 {
				novo = 0
				bits[y*w+x] = true
			}
			e := (old - novo) / 8
			add(x+1, y, e)
			add(x+2, y, e)
			add(x-1, y+1, e)
			add(x, y+1, e)
			add(x+1, y+1, e)
			add(x, y+2, e)
		}
	}
	return bits
}

// imagemParaBits leva a imagem ao bitmap 1-bit que vai para a impressora
// (true = dot preto), com no máx. 576 dots de largura, usando dither Atkinson.
// Compartilhado pela impressão e pelo preview.
func imagemParaBits(img image.Image, aj AjustesImagem) (bits []bool, w, h int, err error) {
	b := img.Bounds()
	if b.Dx()*b.Dy() > maxPixels {
		return nil, 0, 0, fmt.Errorf("imagem: muito grande (%dx%d px); reduza antes de enviar", b.Dx(), b.Dy())
	}
	gray := prepararImagem(img, aj)
	h = len(gray)
	w = len(gray[0])
	return ditherAtkinson(gray), w, h, nil
}

// imagemParaGSv0 leva uma imagem decodificada ao raster GS v 0 (1-bit,
// largura <= 576 dots, proporção mantida, dither Atkinson). alinhamento:
// esquerda | centro | direita.
func imagemParaGSv0(img image.Image, alinhamento string) ([]byte, error) {
	return imagemParaGSv0Ajustes(img, alinhamento, AjustesImagem{})
}

func imagemParaGSv0Ajustes(img image.Image, alinhamento string, aj AjustesImagem) ([]byte, error) {
	bits, w, h, err := imagemParaBits(img, aj)
	if err != nil {
		return nil, err
	}
	bits, w = alinharBits(bits, w, h, alinhamento)
	return append(gsv0Header(w, h), gsv0Data(bits, w, h)...), nil
}

// imagemPreviewPNG renderiza o bitmap 1-bit da impressão como PNG: é o que a
// impressora recebe, sem o respiro lateral. escalaPx > 0 = quantos pixels da
// tela equivalem aos 576 dots do papel: o resultado é reduzido por essa razão (média de área): o preview da tela é bem menor que 576 dots e o
// navegador, ao reduzir um bitmap 1-bit, gera um ruído de aliasing que não
// existe no papel — assim a prévia mostra o que o olho enxerga naquele tamanho.
// Devolve também a cobertura de preto (0..1): fração de dots queimados.
func imagemPreviewPNG(img image.Image, aj AjustesImagem, escalaPx int) ([]byte, float64, error) {
	bits, w, h, err := imagemParaBits(img, aj)
	if err != nil {
		return nil, 0, err
	}
	preto := 0
	out := image.NewGray(image.Rect(0, 0, w, h))
	for i, b := range bits {
		if b {
			preto++
		} else {
			out.Pix[i] = 255
		}
	}
	var res image.Image = out
	if escalaPx > 0 && escalaPx < dotWidth {
		novoW := int(math.Round(float64(w) * float64(escalaPx) / dotWidth))
		if novoW < 1 {
			novoW = 1
		}
		// borra levemente antes de reduzir: dissolve os pontos isolados do dither,
		// que na tela pequena virariam "bolinhas" (aliasing) inexistentes no papel
		res = imaging.Resize(imaging.Blur(out, 0.9), novoW, 0, imaging.Lanczos)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, res); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), float64(preto) / float64(len(bits)), nil
}

// novoQR gera o QR SEM a quiet zone (borda branca de 4 módulos): no cupom o
// papel em volta já é branco e a borda só gastava papel. Usado pela impressão e
// pelo preview para que ambos tenham exatamente o mesmo tamanho.
func novoQR(conteudo string) (*qrcode.QRCode, error) {
	q, err := qrcode.New(conteudo, qrcode.Medium)
	if err != nil {
		return nil, err
	}
	q.DisableBorder = true
	return q, nil
}

// qrModulo normaliza o módulo do QR e o reduz se o QR de n módulos não couber
// em 576 dots. 0 (campo ausente) = padrão 14; qualquer outro valor é limitado a
// qrModMin..qrModMax SEM erro (2 vira 3, 99 vira 23). Compartilhado pela
// impressão e pelo preview (/qr) para que ambos tenham o mesmo tamanho em dots.
func qrModulo(n, modSize int) int {
	mod := modSize
	switch {
	case mod == 0:
		mod = qrModPadrao
	case mod < qrModMin:
		mod = qrModMin
	case mod > qrModMax:
		mod = qrModMax
	}
	// reduz mantendo módulos uniformes — escalar a imagem inteira quebraria
	// a proporção e a legibilidade.
	if n*mod > dotWidth {
		mod = dotWidth / n
		if mod < 1 {
			mod = 1
		}
	}
	return mod
}

// qrParaGSv0 gera o QR do conteúdo e o serializa como raster GS v 0 (fallback
// por imagem — funciona em qualquer impressora ESC/POS, sem depender do
// suporte a GS ( k da i9). modSize é o tamanho do módulo (3..23; padrão 14);
// se o QR não couber em 576 dots, o módulo é reduzido automaticamente.
func qrParaGSv0(conteudo string, modSize int, alinhamento string) ([]byte, error) {
	if strings.TrimSpace(conteudo) == "" {
		return nil, errors.New("qr: conteúdo vazio")
	}
	q, err := novoQR(conteudo)
	if err != nil {
		return nil, fmt.Errorf("qr: conteúdo inválido: %w", err)
	}
	matriz := q.Bitmap()
	n := len(matriz)

	mod := qrModulo(n, modSize)

	w := n * mod
	h := w + qrMargemInferior // linhas finais em branco
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
	q, err := novoQR(conteudo)
	if err != nil {
		return nil, err
	}
	n := len(q.Bitmap())
	modSize = qrModulo(n, modSize)
	lado := n * modSize
	// mesma margem inferior da impressão: o preview mostra exatamente o que sai
	tela := image.NewGray(image.Rect(0, 0, lado, lado+qrMargemInferior))
	draw.Draw(tela, tela.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(tela, image.Rect(0, 0, lado, lado), q.Image(lado), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, tela); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// gerarTesteDegradé cria uma imagem PNG com degradê de tons de cinza (16 níveis)
// para testar quantos tons a impressora consegue distinguir. Cada bloco tem
// um tom diferente, numerado de 0 (branco) a 15 (preto).
func gerarTesteDegradé() ([]byte, error) {
	width := 576  // largura do papel
	height := 480 // altura em pixels

	img := image.NewGray(image.Rect(0, 0, width, height))

	blockWidth := width / 16 // 16 tons de cinza

	for tone := 0; tone < 16; tone++ {
		// Tom de cinza: 0 = branco (255), 15 = preto (0)
		grayValue := 255 - (tone * 255 / 15)
		col := color.Gray{Y: uint8(grayValue)}

		// Desenha o bloco
		x0 := tone * blockWidth
		x1 := x0 + blockWidth
		draw.Draw(img, image.Rect(x0, 0, x1, height), image.NewUniform(col), image.Point{}, draw.Src)
	}

	// Codifica para PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("erro ao gerar degradê: %w", err)
	}
	return buf.Bytes(), nil
}

// qrModulos devolve quantos módulos de lado o QR do conteúdo tem (sem borda).
// A Web UI usa isso para mostrar quanto do papel cada módulo ocuparia.
func qrModulos(conteudo string) (int, error) {
	q, err := novoQR(conteudo)
	if err != nil {
		return 0, err
	}
	return len(q.Bitmap()), nil
}
