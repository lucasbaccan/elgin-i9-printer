//go:build windows

// Suporte Windows: envia os bytes ESC/POS crus pela FILA de impressão via
// winspool (job RAW). Assim funciona com qualquer driver instalado — o driver
// da Elgin ou "Generic / Text Only". ELGIN_LP = nome da fila (ex.: "Elgin I9");
// sem ELGIN_LP usa a impressora padrão do Windows (modo genérico).
//
// Não há detecção por USB ID no Windows (exigiria SetupAPI): o modo genérico
// é simplesmente a impressora padrão. Com mais de uma térmica, defina
// ELGIN_LP com o nome da fila desejada.
package main

import (
	"fmt"
	"io"
	"syscall"
	"unsafe"
)

var (
	winspool              = syscall.NewLazyDLL("winspool.drv")
	procOpenPrinter       = winspool.NewProc("OpenPrinterW")
	procStartDocPrinter   = winspool.NewProc("StartDocPrinterW")
	procWritePrinter      = winspool.NewProc("WritePrinter")
	procEndDocPrinter     = winspool.NewProc("EndDocPrinter")
	procClosePrinter      = winspool.NewProc("ClosePrinter")
	procGetDefaultPrinter = winspool.NewProc("GetDefaultPrinterW")
)

// lpDefault no Windows é "" = fila padrão do sistema.
func lpDefault() string { return "" }

// detectarImpressora no Windows: usa a impressora padrão (modo genérico).
func detectarImpressora() string {
	n, err := filaPadrao()
	if err != nil || n == "" {
		return ""
	}
	return n
}

func filaPadrao() (string, error) {
	var size uint32
	procGetDefaultPrinter.Call(0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return "", fmt.Errorf("sem impressora padrão")
	}
	buf := make([]uint16, size)
	r, _, err := procGetDefaultPrinter.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return "", err
	}
	return syscall.UTF16ToString(buf), nil
}

// devicePresent: tenta abrir a fila (a porta raw não é "statável").
func devicePresent() bool {
	h, err := abrirFila(LP)
	if err != nil {
		return false
	}
	fecharFila(h)
	return true
}

// winPrinter implementa io.WriteCloser sobre a fila do Windows: o primeiro
// Write inicia o job RAW e o Close finaliza.
type winPrinter struct {
	h       uintptr
	started bool
}

func (w *winPrinter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if !w.started {
		if err := iniciarDocumento(w.h); err != nil {
			return 0, err
		}
		w.started = true
	}
	var escrito uint32
	r, _, err := procWritePrinter.Call(w.h, uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)), uintptr(unsafe.Pointer(&escrito)))
	if r == 0 {
		return 0, err
	}
	return int(escrito), nil
}

func (w *winPrinter) Close() error {
	if w.started {
		procEndDocPrinter.Call(w.h)
	}
	fecharFila(w.h)
	return nil
}

// abrirSaida abre a fila configurada (ELGIN_LP) ou a padrão.
func abrirSaida() (io.WriteCloser, error) {
	nome := LP
	if nome == "" {
		d, err := filaPadrao()
		if err != nil {
			return nil, fmt.Errorf("ELGIN_LP vazio e sem impressora padrão: %w", err)
		}
		nome = d
	}
	h, err := abrirFila(nome)
	if err != nil {
		return nil, fmt.Errorf("abrindo fila %q: %w", nome, err)
	}
	return &winPrinter{h: h}, nil
}

func abrirFila(nome string) (uintptr, error) {
	var h uintptr
	np, err := syscall.UTF16PtrFromString(nome)
	if err != nil {
		return 0, err
	}
	r, _, e := procOpenPrinter.Call(uintptr(unsafe.Pointer(np)), uintptr(unsafe.Pointer(&h)), 0)
	if r == 0 {
		return 0, e
	}
	return h, nil
}

func fecharFila(h uintptr) { procClosePrinter.Call(h) }

type docInfo1W struct {
	DocName    *uint16
	OutputFile *uint16
	Datatype   *uint16
}

func iniciarDocumento(h uintptr) error {
	doc, _ := syscall.UTF16PtrFromString("elgin-print")
	raw, _ := syscall.UTF16PtrFromString("RAW")
	info := docInfo1W{DocName: doc, Datatype: raw}
	r, _, err := procStartDocPrinter.Call(h, 1, uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return err
	}
	return nil
}
