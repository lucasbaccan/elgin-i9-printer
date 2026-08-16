//go:build !windows

// Detecção da impressora e acesso ao device em Linux/macOS/BSD.
//
// O driver usblp expõe cada impressora USB como /dev/usb/lpN com um link no
// sysfs (/sys/class/usbmisc/lpN; kernels antigos: /sys/class/usb/lpN) que
// aponta para a interface USB. Subindo do link até o device USB pai
// encontramos o idVendor:idProduct — é assim que identificamos a Elgin I9
// (20d1:7008). Se o ID não for encontrado, cai no modo genérico: a primeira
// impressora USB disponível (ordem numérica).
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Elgin I9: idVendor 20d1, idProduct 7008.
const (
	elginVendor  = "20d1"
	elginProduct = "7008"
)

// lpDefault é o device usado quando nada é detectado (compatível com o
// comportamento antigo).
func lpDefault() string { return "/dev/usb/lp0" }

// detectarImpressora procura a Elgin I9 entre as impressoras USB do sistema.
// Não achando, devolve a primeira impressora USB (modo genérico). Devolve ""
// se não existir nenhuma — aí o lpDefault é mantido e o /health reporta.
func detectarImpressora() string {
	return detectarEm("/sys/class/usbmisc", "/sys/class/usb")
}

// detectarEm procura nos diretórios sysfs passados (separável para testes).
func detectarEm(dirs ...string) string {
	var lps []string
	for _, d := range dirs {
		m, _ := filepath.Glob(filepath.Join(d, "lp*"))
		lps = append(lps, m...)
	}
	if len(lps) == 0 {
		return ""
	}
	// ordem numérica: lp2 antes de lp10
	sort.Slice(lps, func(i, j int) bool { return lpNum(lps[i]) < lpNum(lps[j]) })
	// 1) procura a Elgin I9 por USB ID
	for _, sys := range lps {
		if usbIDDoLp(sys) == elginVendor+":"+elginProduct {
			return deviceDe(sys)
		}
	}
	// 2) modo genérico: primeira impressora USB disponível
	return deviceDe(lps[0])
}

// deviceDe converte o nome do sysfs (lpN) no device usblp (/dev/usb/lpN).
func deviceDe(sys string) string {
	return "/dev/usb/" + filepath.Base(sys)
}

// lpNum extrai o número do lpN (para ordenar lp10 depois de lp2).
func lpNum(sys string) int {
	n := 0
	for _, r := range filepath.Base(sys) {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		}
	}
	return n
}

// usbIDDoLp sobe do link sysfs do lp até o device USB pai e devolve
// "idVendor:idProduct" ("" se não achar — ex.: interface sem USB pai).
func usbIDDoLp(sys string) string {
	real, err := filepath.EvalSymlinks(sys)
	if err != nil {
		return ""
	}
	for dir := real; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		v, err1 := os.ReadFile(filepath.Join(dir, "idVendor"))
		p, err2 := os.ReadFile(filepath.Join(dir, "idProduct"))
		if err1 == nil && err2 == nil {
			return strings.TrimSpace(string(v)) + ":" + strings.TrimSpace(string(p))
		}
	}
	return ""
}

// devicePresent informa se a impressora está disponível agora (estado REAL,
// usado pelo /health).
func devicePresent() bool {
	_, err := os.Stat(LP)
	return err == nil
}

// abrirSaida abre o device da impressora para escrita (usblp).
func abrirSaida() (io.WriteCloser, error) {
	f, err := os.OpenFile(LP, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("abrindo %s: %w", LP, err)
	}
	return f, nil
}
