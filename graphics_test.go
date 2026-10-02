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
	// quadrado + margem inferior: altura = largura em dots + qrMargemInferior
	wBytes := int(out[4]) | int(out[5])<<8
	hDots := int(out[6]) | int(out[7])<<8
	if wBytes != (hDots-qrMargemInferior+7)/8 {
		t.Fatalf("QR deveria ser quadrado + margem: %d bytes de largura vs %d dots de altura", wBytes, hDots)
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
	wDots := hDots - qrMargemInferior // quadrado: largura em dots == altura sem a margem
	bits := decodeGsv0(out[8:], wDots, hDots)

	// sem quiet zone: largura = n * mod, mod = 4
	mod := 4
	n := wDots / mod
	if n < 21 {
		t.Fatalf("tamanho do QR inesperado (versão muito pequena): %d módulos", n)
	}

	// finder pattern fica nos módulos [0..6] (7 de finder, sem quiet zone)
	// conversão módulo -> pixel: pixel = módulo * mod (canto superior esquerdo)
	get := func(my, mx int) bool { return bits[my*mod][mx*mod] }
	// canto e centro do finder
	if !get(0, 0) {
		t.Fatal("canto superior esquerdo do finder deveria ser preto")
	}
	if !get(0, 6) {
		t.Fatal("canto superior direito do finder deveria ser preto")
	}
	if !get(6, 0) {
		t.Fatal("canto inferior esquerdo do finder deveria ser preto")
	}
	if !get(3, 3) { // centro do finder (3x3 preto)
		t.Fatal("centro do finder deveria ser preto")
	}
	if get(1, 1) { // anel branco interno
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

// true = dot preto: pixel escuro deve virar tinta, claro deve ficar branco
// (regressão: os dithers estavam com a polaridade invertida na impressão).
func TestDitherPolaridade(t *testing.T) {
	const w, h = 16, 16
	mk := func(v uint8) [][]uint8 {
		g := make([][]uint8, h)
		for y := range g {
			g[y] = make([]uint8, w)
			for x := range g[y] {
				g[y][x] = v
			}
		}
		return g
	}
	conta := func(bits []bool) (n int) {
		for _, b := range bits {
			if b {
				n++
			}
		}
		return
	}
	for nome, fn := range map[string]func([][]uint8) []bool{"atkinson": ditherAtkinson} {
		if n := conta(fn(mk(0))); n != w*h {
			t.Errorf("%s: preto puro deveria ser 100%% tinta, veio %d/%d", nome, n, w*h)
		}
		if n := conta(fn(mk(255))); n != 0 {
			t.Errorf("%s: branco puro não deveria ter tinta, veio %d", nome, n)
		}
		if n := conta(fn(mk(128))); n < w*h/4 || n > w*h*3/4 {
			t.Errorf("%s: cinza médio deveria ter ~50%% de tinta, veio %d/%d", nome, n, w*h)
		}
	}
}

func TestQrModulosEModuloGrande(t *testing.T) {
	n, err := qrModulos("https://www.google.com")
	if err != nil || n != 25 {
		t.Fatalf("QR do google deveria ter 25 módulos sem borda, veio %d (%v)", n, err)
	}
	// módulos acima de 8 agora são aceitos (até o que cabe nos 576 dots)
	if m := qrModulo(25, 20); m != 20 {
		t.Fatalf("módulo 20 deveria caber (25*20=500 dots), veio %d", m)
	}
	if m := qrModulo(25, 30); m != 23 {
		t.Fatalf("módulo 30 deveria ser reduzido a 23 (576/25), veio %d", m)
	}
}

// o preview "como será impresso" deve mostrar escuro = preto e claro = branco.
func TestImagemPreviewPNG(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			if x < 8 {
				img.SetGray(x, y, color.Gray{Y: 0}) // metade esquerda preta
			} else {
				img.SetGray(x, y, color.Gray{Y: 255}) // metade direita branca
			}
		}
	}
	for _, d := range []string{"atkinson"} {
		out, _, err := imagemPreviewPNG(img, AjustesImagem{}, 0)
		if err != nil {
			t.Fatal(err)
		}
		dec, err := png.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("%s: PNG inválido: %v", d, err)
		}
		if r, _, _, _ := dec.At(2, 2).RGBA(); r != 0 {
			t.Errorf("%s: lado escuro deveria ser preto", d)
		}
		if r, _, _, _ := dec.At(12, 2).RGBA(); r == 0 {
			t.Errorf("%s: lado claro deveria ser branco", d)
		}
	}
}

func imgCinza(w, h int, v uint8) image.Image {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = v
	}
	return img
}

func tinta(bits []bool) (n int) {
	for _, b := range bits {
		if b {
			n++
		}
	}
	return
}

func TestAjustesBrilhoInverterLargura(t *testing.T) {
	semAuto := false
	base := AjustesImagem{AutoContraste: &semAuto}
	cinza := imgCinza(64, 64, 100)

	b0, _, _, _ := imagemParaBits(cinza, base)
	claro := base
	claro.Brilho = 100
	b1, _, _, _ := imagemParaBits(cinza, claro)
	if tinta(b1) >= tinta(b0) {
		t.Errorf("brilho +100 deveria diminuir a tinta: %d -> %d", tinta(b0), tinta(b1))
	}
	escuro := base
	escuro.Brilho = -100
	b2, _, _, _ := imagemParaBits(cinza, escuro)
	if tinta(b2) <= tinta(b0) {
		t.Errorf("brilho -100 deveria aumentar a tinta: %d -> %d", tinta(b0), tinta(b2))
	}
	inv := base
	inv.Inverter = true
	b3, _, _, _ := imagemParaBits(imgCinza(64, 64, 0), inv)
	if tinta(b3) != 0 {
		t.Errorf("preto invertido deveria ser branco puro, veio %d dots de tinta", tinta(b3))
	}
	// largura pedida (amplia e reduz), sempre limitada ao papel
	for _, c := range []struct{ pedido, esperado int }{{200, 200}, {32, 32}, {9999, dotWidth}, {0, 64}} {
		aj := base
		aj.Largura = c.pedido
		_, w, _, err := imagemParaBits(cinza, aj)
		if err != nil || w != c.esperado {
			t.Errorf("largura %d: esperado %d dots, veio %d (%v)", c.pedido, c.esperado, w, err)
		}
	}
}

func TestImagemTransparenteViraBranco(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32)) // totalmente transparente
	bits, _, _, err := imagemParaBits(img, AjustesImagem{})
	if err != nil {
		t.Fatal(err)
	}
	if n := tinta(bits); n != 0 {
		t.Errorf("fundo transparente deveria imprimir branco, veio %d dots de tinta", n)
	}
}

// o QR não pode terminar rente ao corte/texto seguinte: sobram linhas brancas embaixo.
func TestQrTemMargemInferiorEmBranco(t *testing.T) {
	out, err := qrParaGSv0("https://exemplo.com", 4, "esquerda")
	if err != nil {
		t.Fatal(err)
	}
	wBytes := int(out[4]) | int(out[5])<<8
	hDots := int(out[6]) | int(out[7])<<8
	for _, b := range out[8+wBytes*(hDots-qrMargemInferior):] {
		if b != 0 {
			t.Fatal("as linhas finais do QR deveriam ser brancas")
		}
	}
	// a última linha com tinta tem que estar acima da margem
	if !bytes.Contains(out[8+wBytes*(hDots-qrMargemInferior-4):8+wBytes*(hDots-qrMargemInferior)], []byte{0xff}) {
		t.Fatal("o QR deveria terminar logo antes da margem")
	}
}

// qr_tamanho fora do intervalo não dá erro: é ajustado para 3..23 (0 = padrão 4).
func TestQrModuloLimitaSemErro(t *testing.T) {
	for _, c := range []struct{ pedido, esperado int }{
		{0, 14}, {-5, 3}, {1, 3}, {2, 3}, {3, 3}, {10, 10}, {23, 23}, {24, 23}, {99, 23},
	} {
		if m := qrModulo(21, c.pedido); m != c.esperado {
			t.Errorf("qr_tamanho %d: esperado módulo %d, veio %d", c.pedido, c.esperado, m)
		}
	}
	// QR grande: reduz para caber, mas nunca abaixo de 3 (maior QR = 177 módulos)
	if m := qrModulo(177, 23); m != 3 {
		t.Errorf("QR de 177 módulos deveria caber com módulo 3, veio %d", m)
	}
	if _, err := qrParaGSv0("https://exemplo.com", 99, "centro"); err != nil {
		t.Errorf("qr_tamanho 99 não deveria dar erro: %v", err)
	}
	if _, err := qrParaGSv0("https://exemplo.com", 1, "centro"); err != nil {
		t.Errorf("qr_tamanho 1 não deveria dar erro: %v", err)
	}
}
