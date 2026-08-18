//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeSysfs monta a árvore sysfs fake:
//
//	class/lp0 -> real/1-4/1-4:1.0/usbmisc/lp0   (interface usblp)
//	real/1-4/idVendor + idProduct                (device USB pai)
func fakeSysfs(t *testing.T, vendor, product string) string {
	t.Helper()
	base := t.TempDir()
	class := filepath.Join(base, "class")
	real := filepath.Join(base, "real", "1-4", "1-4:1.0", "usbmisc")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(class, 0o755); err != nil {
		t.Fatal(err)
	}
	dev := filepath.Join(base, "real", "1-4")
	if vendor != "" {
		if err := os.WriteFile(filepath.Join(dev, "idVendor"), []byte(vendor), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if product != "" {
		if err := os.WriteFile(filepath.Join(dev, "idProduct"), []byte(product), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(real, "lp0"), filepath.Join(class, "lp0")); err != nil {
		t.Fatal(err)
	}
	return class
}

func TestDetectarElginPorID(t *testing.T) {
	class := fakeSysfs(t, "20d1", "7008")
	if got := detectarEm(class); got != "/dev/usb/lp0" {
		t.Fatalf("deveria achar a Elgin em /dev/usb/lp0, veio %q", got)
	}
}

func TestDetectarModoGenerico(t *testing.T) {
	// impressora de outro fabricante (sem o ID da Elgin): modo genérico
	class := fakeSysfs(t, "04b8", "0202")
	if got := detectarEm(class); got != "/dev/usb/lp0" {
		t.Fatalf("modo genérico deveria devolver /dev/usb/lp0, veio %q", got)
	}
}

func TestDetectarSemImpressora(t *testing.T) {
	if got := detectarEm(t.TempDir()); got != "" {
		t.Fatalf("sem lp deveria devolver \"\", veio %q", got)
	}
}

func TestDetectarLpSemID(t *testing.T) {
	// interface sem device USB pai (idVendor ausente): não pode quebrar
	class := fakeSysfs(t, "", "")
	if got := detectarEm(class); got != "/dev/usb/lp0" {
		t.Fatalf("sem ID deveria cair no modo genérico /dev/usb/lp0, veio %q", got)
	}
}

func TestDetectarOrdemNumerica(t *testing.T) {
	// lp10 não pode vencer lp2 no modo genérico
	base := t.TempDir()
	class := filepath.Join(base, "class")
	if err := os.MkdirAll(class, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"lp2", "lp10"} {
		r := filepath.Join(base, "real"+n)
		iface := filepath.Join(r, "iface", "usbmisc")
		if err := os.MkdirAll(iface, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r, "idVendor"), []byte("0000"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r, "idProduct"), []byte("0000"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(iface, n), filepath.Join(class, n)); err != nil {
			t.Fatal(err)
		}
	}
	if got := detectarEm(class); got != "/dev/usb/lp2" {
		t.Fatalf("genérico deveria escolher lp2 (ordem numérica), veio %q", got)
	}
}

func TestLPNum(t *testing.T) {
	cases := []struct{ in, out int }{
		{lpNum("/sys/class/usbmisc/lp0"), 0},
		{lpNum("/sys/class/usbmisc/lp2"), 2},
		{lpNum("/sys/class/usbmisc/lp10"), 10},
	}
	for _, c := range cases {
		if c.in != c.out {
			t.Fatalf("lpNum deu %d, esperado %d", c.in, c.out)
		}
	}
}
