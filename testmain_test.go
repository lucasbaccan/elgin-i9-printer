package main

import (
	"os"
	"testing"
)

// TestMain aponta LP para um arquivo temporário que existe: assim devicePresent()
// é verdadeiro e os testes dos endpoints (que trocam `enviar` por um falso) passam
// em qualquer máquina, com ou sem impressora conectada. Testes que precisam de um
// device ausente ou específico definem LP por conta própria.
func TestMain(m *testing.M) {
	f, err := os.CreateTemp("", "lp-fake-*")
	if err != nil {
		os.Exit(m.Run())
	}
	f.Close()
	LP = f.Name()
	code := m.Run()
	os.Remove(f.Name())
	os.Exit(code)
}
