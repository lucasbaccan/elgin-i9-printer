BINARY := elgin-print
GO ?= go
PORT ?= 8000

.PHONY: build test vet cross clean run dev test-print help

build:
	$(GO) build -trimpath -ldflags "-s -w" -o $(BINARY) .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

# Binário estático para linux/amd64 (roda em Alpine/musl sem dependências).
cross:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w" -o $(BINARY) .

clean:
	rm -f $(BINARY)

# Compila e roda o servidor da Web UI + API REST na porta
run: build
	ELGIN_API_PORT=$(PORT) ./$(BINARY) serve

# Imprime um cupom de teste para validar a impressora conectada
test-print: build
	./$(BINARY) print
