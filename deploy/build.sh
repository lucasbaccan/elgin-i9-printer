#!/bin/sh
# Cross-compila o binário estático para linux/amd64 (Alpine/musl, zero deps).
set -e
cd "$(dirname "$0")/.."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o elgin-print .
echo "Binário gerado: ./elgin-print ($(du -h elgin-print | cut -f1))"
