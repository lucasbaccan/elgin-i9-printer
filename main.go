package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// LP é a impressora em uso (sobrescrevível via ELGIN_LP):
//   - Linux/macOS/BSD: device usblp (ex: /dev/usb/lp0)
//   - Windows: nome da fila de impressão (vazio = fila padrão)
var LP = lpDefault()

func main() {
	if v := os.Getenv("ELGIN_LP"); v != "" {
		LP = v
	} else if d := detectarImpressora(); d != "" {
		LP = d
		if d != lpDefault() {
			fmt.Fprintf(os.Stderr, "elgin-print: impressora detectada automaticamente: %s\n", d)
		}
	}
	if v := os.Getenv("ELGIN_LINE_DELAY_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms >= 0 {
			lineDelay = time.Duration(ms) * time.Millisecond
		}
	}

	if len(os.Args) < 2 {
		usage()
		return
	}

	switch os.Args[1] {
	case "print":
		cmdPrint(os.Args[2:])
	case "serve":
		cmdServe()
	case "feed":
		cmdFeed(os.Args[2:])
	case "cut":
		cmdCut()
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "subcomando desconhecido: %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Print(`elgin-print — impressora térmica Elgin I9 (ESC/POS via USB)

Uso:
  elgin-print print [-t TITULO] [-e|-c|-d TEXTO]...
  elgin-print serve
  elgin-print feed [N]
  elgin-print cut

Subcomandos:
  print   imprime um cupom (sem argumentos = cupom de teste)
  serve   sobe a API REST + Web UI na porta ELGIN_API_PORT (default 8000)
  feed    avança N linhas de papel (default 3)
  cut     aciona a guilhotina (GS V 66 0)

Flags do print:
  -t TITULO   título em largura 2x (centralizado)
  -e TEXTO    linha alinhada à esquerda
  -c TEXTO    linha centralizada
  -d TEXTO    linha alinhada à direita
  -f          respiro de 3 linhas antes do corte (sem = corte rente, compacto)
  (texto sem flag = centralizado)

Env:
  ELGIN_LP        impressora em uso (Linux: /dev/usb/lpN; Windows: nome da fila).
                  Vazio = detecção automática: acha a Elgin I9 por USB ID
                  (20d1:7008); não achando, usa a primeira impressora USB
                  (modo genérico). Default sem detecção: /dev/usb/lp0.
  ELGIN_API_PORT  porta da API/Web UI (default 8000)
`)
}

// cmdPrint parseia -e/-c/-d/-t (intercaláveis) e imprime. Sem argumentos,
// imprime o cupom de teste.
func cmdPrint(args []string) {
	type item struct {
		align string
		text  string
	}
	var titulo string
	var itens []item
	feedCorte := 0

	if len(args) == 0 {
		if err := enviar(cupomTeste(), true, feedCorte); err != nil {
			fatal(err.Error())
		}
		fmt.Println("OK - cupom de teste impresso!")
		return
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-f":
			feedCorte = feedCortePadrao
		case "-t":
			i++
			if i >= len(args) {
				fatal("falta texto depois de -t")
			}
			titulo = args[i]
		case "-e", "-c", "-d":
			i++
			if i >= len(args) {
				fatal(fmt.Sprintf("falta texto depois de %s", a))
			}
			itens = append(itens, item{align: a, text: args[i]})
		default:
			itens = append(itens, item{align: "-c", text: a})
		}
	}

	if len(itens) == 0 && titulo == "" {
		itens = append(itens, item{align: "-c", text: "MENSAGEM DO HERMES"})
	}

	linhas := make([]Linha, 0, len(itens))
	for _, it := range itens {
		al := "centro"
		switch it.align {
		case "-e":
			al = "esquerda"
		case "-d":
			al = "direita"
		}
		linhas = append(linhas, Linha{Texto: it.text, Alinhamento: al})
	}

	dados, err := montarCupom(titulo, linhas)
	if err != nil {
		fatal(err.Error())
	}
	if err := enviar(dados, true, feedCorte); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("OK - %d linha(s) impressa(s)!\n", len(linhas))
}

func cmdFeed(args []string) {
	n := 3
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err != nil {
			fatal(fmt.Sprintf("N inválido: %q", args[0]))
		}
		n = v
	}
	if err := enviar(feedBytes(n), false, 0); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("OK - %d linha(s) avançada(s)!\n", n)
}

func cmdCut() {
	if err := enviar(cutBytes(), false, 0); err != nil {
		fatal(err.Error())
	}
	fmt.Println("OK - corte acionado!")
}

func fatal(s string) {
	fmt.Fprintln(os.Stderr, "ERRO:", s)
	os.Exit(1)
}
