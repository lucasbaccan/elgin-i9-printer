package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// novaImagemPNG gera os bytes PNG de uma imagem w x h pintada por `paint`.
func novaImagemPNG(w, h int, paint func(x, y int) color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, paint(x, y))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// branca devolve uma imagem toda branca (imprime "nada", só avança papel).
func branca(w, h int) []byte {
	return novaImagemPNG(w, h, func(x, y int) color.Color { return color.White })
}

// decodeGsv0 reverte gsv0Data para um bitmap [][]bool (só para teste).
func decodeGsv0(data []byte, w, h int) [][]bool {
	widthBytes := (w + 7) / 8
	bits := make([][]bool, h)
	for y := 0; y < h; y++ {
		bits[y] = make([]bool, w)
		for bx := 0; bx < widthBytes; bx++ {
			b := data[y*widthBytes+bx]
			for i := 0; i < 8; i++ {
				x := bx*8 + i
				if x < w {
					bits[y][x] = b&(1<<(7-i)) != 0
				}
			}
		}
	}
	return bits
}

func TestDecodeImagemPNG(t *testing.T) {
	raw := branca(10, 20)
	b64 := base64.StdEncoding.EncodeToString(raw)
	img, err := decodeImagem(b64)
	if err != nil {
		t.Fatalf("decodeImagem falhou: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 10 || b.Dy() != 20 {
		t.Fatalf("dimensões esperadas 10x20, veio %dx%d", b.Dx(), b.Dy())
	}
}

func TestDecodeImagemDataURL(t *testing.T) {
	raw := branca(5, 5)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	img, err := decodeImagem(dataURL)
	if err != nil {
		t.Fatalf("decodeImagem com prefixo data: falhou: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 5 {
		t.Fatalf("largura esperada 5, veio %d", b.Dx())
	}
}

func TestDecodeImagemInvalido(t *testing.T) {
	if _, err := decodeImagem("!!!não-é-base64!!!"); err == nil {
		t.Fatal("base64 inválido deveria retornar erro")
	}
	if _, err := decodeImagem(base64.StdEncoding.EncodeToString([]byte("não é imagem"))); err == nil {
		t.Fatal("bytes que não são imagem deveriam retornar erro")
	}
}

func TestGsv0Header(t *testing.T) {
	// 8 dots de largura = 1 byte; 8 de altura
	if got := gsv0Header(8, 8); !bytes.Equal(got, []byte{0x1d, 0x76, 0x30, 0x00, 0x01, 0x00, 0x08, 0x00}) {
		t.Fatalf("gsv0Header(8,8) = %x, esperado 1d 76 30 00 01 00 08 00", got)
	}
	// 16 dots de largura = 2 bytes; 16 de altura
	if got := gsv0Header(16, 16); !bytes.Equal(got, []byte{0x1d, 0x76, 0x30, 0x00, 0x02, 0x00, 0x10, 0x00}) {
		t.Fatalf("gsv0Header(16,16) = %x", got)
	}
}

func TestGsv0DataPacoteDeBits(t *testing.T) {
	// 8x8: linha 0 toda preta (0xff), linha 1 toda branca (0x00), linha 2 só o
	// dot mais à esquerda (0x80), linha 3 só o mais à direita (0x01).
	w, h := 8, 8
	bits := make([]bool, w*h)
	for x := 0; x < w; x++ {
		bits[0*w+x] = true
	}
	bits[2*w+0] = true
	bits[3*w+7] = true
	data := gsv0Data(bits, w, h)
	want := []byte{0xff, 0x00, 0x80, 0x01, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(data, want) {
		t.Fatalf("gsv0Data = %x, esperado %x", data, want)
	}
}

func TestGsv0RoundTrip(t *testing.T) {
	// bitmap com padrão quadriculado: codifica e decodifica de volta.
	w, h := 24, 16
	bits := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			bits[y*w+x] = (x/4+y/4)%2 == 0
		}
	}
	data := gsv0Data(bits, w, h)
	back := decodeGsv0(data, w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if back[y][x] != bits[y*w+x] {
				t.Fatalf("round-trip divergiu em (%d,%d): %v != %v", x, y, back[y][x], bits[y*w+x])
			}
		}
	}
}

func TestImagemParaGSv0EscalaParaPapel(t *testing.T) {
	// 1152x576 (2x a largura do papel) -> reduz para 576x288.
	raw := branca(1152, 576)
	img, err := decodeImagem(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	out, err := imagemParaGSv0(img, "esquerda")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x1d, 0x76, 0x30, 0x00, 0x48, 0x00, 0x20, 0x01} // 72 bytes (576 dots) x 288 dots
	if !bytes.HasPrefix(out, want) {
		t.Fatalf("cabeçalho deveria ser %x, veio %x", want, out[:8])
	}
	// total de bytes = 72 (bytes/linha) x 288 (linhas) + 8 do cabeçalho
	if len(out) != 8+72*288 {
		t.Fatalf("tamanho total esperado %d, veio %d", 8+72*288, len(out))
	}
}

func TestImagemParaGSv0CentroPreenchePapel(t *testing.T) {
	// 100x100, alinhamento centro -> preenchido até 576 dots (72 bytes).
	raw := branca(100, 100)
	img, err := decodeImagem(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	out, err := imagemParaGSv0(img, "centro")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte{0x1d, 0x76, 0x30, 0x00, 0x48, 0x00, 0x64, 0x00}) {
		t.Fatalf("imagem centralizada deveria ter 72 bytes de largura, veio %x", out[:8])
	}
}

func TestImagemParaGSv0EsquerdaSemPreencher(t *testing.T) {
	raw := branca(100, 100)
	img, _ := decodeImagem(base64.StdEncoding.EncodeToString(raw))
	out, err := imagemParaGSv0(img, "esquerda")
	if err != nil {
		t.Fatal(err)
	}
	// 100 dots = 13 bytes (ceil(100/8))
	if !bytes.HasPrefix(out, []byte{0x1d, 0x76, 0x30, 0x00, 0x0d, 0x00, 0x64, 0x00}) {
		t.Fatalf("imagem à esquerda não deveria preencher; veio %x", out[:8])
	}
}

func TestQrParaGSv0(t *testing.T) {
	out, err := qrParaGSv0("https://exemplo.com", 4, "esquerda")
	if err != nil {
		t.Fatalf("qrParaGSv0 falhou: %v", err)
	}
	if len(out) < 8 {
		t.Fatal("saída de QR muito curta")
	}
	// cabeçalho GS v 0 m=0
	if !bytes.HasPrefix(out, []byte{0x1d, 0x76, 0x30, 0x00}) {
		t.Fatalf("QR deveria começar com GS v 0, veio %x", out[:4])
	}
	// quadrado: largura em bytes == ceil(altura/8)  (largura em dots == altura)
	wBytes := int(out[4]) | int(out[5])<<8
	hDots := int(out[6]) | int(out[7])<<8
	if wBytes != (hDots+7)/8 {
		t.Fatalf("QR deveria ser quadrado: %d bytes de largura vs %d dots de altura", wBytes, hDots)
	}
	// total de bytes da imagem = largura em bytes x altura em dots
	if len(out)-8 != wBytes*hDots {
		t.Fatalf("tamanho dos dados %d, esperado %d", len(out)-8, wBytes*hDots)
	}
	// dados com pelo menos um bit preto (o finder pattern garante)
	if !bytes.Contains(out[8:], []byte{0xff}) {
		t.Fatal("QR não contém nenhum byte preto — deveria ter o finder pattern")
	}
}

func TestQrParaGSv0Deterministico(t *testing.T) {
	a, _ := qrParaGSv0("mesmo conteúdo", 4, "centro")
	b, _ := qrParaGSv0("mesmo conteúdo", 4, "centro")
	if !bytes.Equal(a, b) {
		t.Fatal("mesmo conteúdo deveria gerar o mesmo QR (determinístico)")
	}
}

func TestQrParaGSv0Vazio(t *testing.T) {
	if _, err := qrParaGSv0("   ", 4, "centro"); err == nil {
		t.Fatal("QR com conteúdo vazio deveria retornar erro")
	}
}

func TestQrFinderPattern(t *testing.T) {
	out, err := qrParaGSv0("https://exemplo.com", 4, "esquerda")
	if err != nil {
		t.Fatal(err)
	}
	hDots := int(out[6]) | int(out[7])<<8
	wDots := hDots // quadrado: largura em dots == altura
	bits := decodeGsv0(out[8:], wDots, hDots)

	// largura = (n + 8) * mod -> n = módulos do QR (sem quiet zone), mod = 4
	mod := 4
	totalMod := hDots / mod
	n := totalMod - 8
	if n < 21 {
		t.Fatalf("tamanho do QR inesperado (versão muito pequena): %d módulos", n)
	}

	// finder pattern fica nos módulos [4..10] (quiet zone 4 + 7 de finder)
	// conversão módulo -> pixel: pixel = módulo * mod (canto superior esquerdo)
	get := func(my, mx int) bool { return bits[my*mod][mx*mod] }
	// canto e centro do finder
	if !get(4, 4) {
		t.Fatal("canto superior esquerdo do finder deveria ser preto")
	}
	if !get(4, 10) {
		t.Fatal("canto superior direito do finder deveria ser preto")
	}
	if !get(10, 4) {
		t.Fatal("canto inferior esquerdo do finder deveria ser preto")
	}
	if !get(7, 7) { // centro do finder (3x3 preto)
		t.Fatal("centro do finder deveria ser preto")
	}
	if get(5, 5) { // anel branco interno
		t.Fatal("anel interno do finder deveria ser branco")
	}
}

func TestQrModuloGrandeCabeNoPapel(t *testing.T) {
	// conteúdo longo + módulo 8: se não couber em 576, o módulo reduz.
	out, err := qrParaGSv0("https://exemplo.com/um/caminho/bem/comprido/para/testar?x=1&y=2&z=3", 8, "esquerda")
	if err != nil {
		t.Fatal(err)
	}
	wBytes := int(out[4]) | int(out[5])<<8
	if wBytes*8 > dotWidth {
		t.Fatalf("QR com módulo 8 deveria caber em %d dots, ficou %d", dotWidth, wBytes*8)
	}
}

func TestMontarCupomComQR(t *testing.T) {
	out, err := montarCupom("PEDIDO", []Linha{
		{Texto: "Pague via PIX:"},
		{Tipo: "qr", Qr: "https://exemplo.com/pix", QrTamanho: 4, Alinhamento: "centro"},
	})
	if err != nil {
		t.Fatalf("montarCupom com QR falhou: %v", err)
	}
	// o texto vem antes e o raster GS v 0 (QR) vem depois
	if !bytes.Contains(out, []byte("Pague via PIX:")) {
		t.Fatal("texto não encontrado")
	}
	if !bytes.Contains(out, []byte{0x1d, 0x76, 0x30, 0x00}) {
		t.Fatal("QR (GS v 0) não encontrado no cupom")
	}
}

func TestMontarCupomComImagem(t *testing.T) {
	raw := branca(50, 30)
	b64 := base64.StdEncoding.EncodeToString(raw)
	out, err := montarCupom("", []Linha{{Tipo: "imagem", Imagem: b64, Alinhamento: "centro"}})
	if err != nil {
		t.Fatalf("montarCupom com imagem falhou: %v", err)
	}
	if !bytes.Contains(out, []byte{0x1d, 0x76, 0x30, 0x00}) {
		t.Fatal("imagem (GS v 0) não encontrada no cupom")
	}
}

func TestMontarCupomImagemInvalida(t *testing.T) {
	if _, err := montarCupom("", []Linha{{Tipo: "imagem", Imagem: "!!!"}}); err == nil {
		t.Fatal("imagem com base64 inválido deveria retornar erro")
	}
	if _, err := montarCupom("", []Linha{{Tipo: "imagem", Imagem: ""}}); err == nil {
		t.Fatal("bloco imagem sem dados deveria retornar erro")
	}
	if _, err := montarCupom("", []Linha{{Tipo: "qr", Qr: ""}}); err == nil {
		t.Fatal("QR sem conteúdo deveria retornar erro")
	}
}

func TestMontarCupomTipoDesconhecidoViraTexto(t *testing.T) {
	// tipo desconhecido (ou "texto" explícito) cai no default de texto
	out, err := montarCupom("", []Linha{{Tipo: "texto", Texto: "oi"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("oi")) {
		t.Fatal("tipo texto deveria imprimir o texto")
	}
}
