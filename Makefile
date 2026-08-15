BINARY := elgin-print
GO ?= go

.PHONY: build test vet cross clean

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
