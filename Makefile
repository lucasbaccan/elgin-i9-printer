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

# Hot reload: ao salvar .go ou webui/*.html recompila e sobe de novo (Air)
dev:
	ELGIN_API_PORT=$(PORT) $(GO) run github.com/air-verse/air@latest

help:
	@echo "Comandos disponíveis:"
	@echo "  make build      - Compila o binário"
	@echo "  make run        - Compila e roda o servidor (porta $(PORT))"
	@echo "  make test-print - Compila e imprime um cupom de teste"
	@echo "  make test       - Roda os testes"
	@echo "  make vet        - Verifica o código com 'go vet'"
	@echo "  make cross      - Compila para linux/amd64 (estático)"
	@echo "  make clean      - Remove o binário compilado"
	@echo ""
	@echo "Desenvolvimento (auto-recompila):"
	@echo "  make dev        - Hot reload (Air): recompila e sobe de novo ao salvar .go/.html"
	@echo ""
	@echo "Variáveis de ambiente:"
	@echo "  PORT=8080 make run    - Roda na porta 8080 (default: 8000)"
	@echo "  ELGIN_LP=/dev/usb/lp1 - Especifica o device da impressora"
