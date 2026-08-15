# Elgin I9 Printer 🖨️

Serviço de impressão para a **impressora térmica Elgin I9** (ESC/POS via USB) do homelab.

- **CLI**: `imprimir_elgin.sh` — imprime cupons direto no `/dev/usb/lp0`
- **API REST**: `api/main.py` (FastAPI) — imprime pela rede, sem precisar acessar a máquina

## Como usar (CLI)

```bash
./imprimir_elgin.sh                       # cupom de teste completo
./imprimir_elgin.sh "texto"               # mensagem centralizada + moldura + corte
./imprimir_elgin.sh -e "esq" -c "centro" -d "dir"   # multilinha com alinhamentos
./imprimir_elgin.sh -t "Título" -c "texto"           # título 2x + corpo
```

O corte é automático (`GS V 49`): o papel avança até a guilhotina e corta sozinho. ✂️

## Como usar (API)

```bash
# health check
curl http://<host>:8000/health

# cupom de teste
curl -X POST http://<host>:8000/test

# cupom personalizado
curl -X POST http://<host>:8000/print \
  -H 'Content-Type: application/json' \
  -d '{
    "titulo": "PEDIDO #123",
    "linhas": [
      {"texto": "1x Hamburguer", "alinhamento": "esquerda"},
      {"texto": "2x Refrigerante", "alinhamento": "esquerda"},
      {"texto": "TOTAL: R$ 45,00", "alinhamento": "direita"}
    ]
  }'
```

## Instalação na VM/LXC

```bash
sudo ./deploy/setup_vm.sh
```

Cria venv, instala o serviço systemd (`elgin-print-api`) e sobe a API na porta 8000.

## Roadmap

- [x] Script CLI de impressão (corte, alinhamento, fontes)
- [x] API REST básica (FastAPI)
- [ ] VM dedicada com USB passthrough nativo (sem restart ao religar a impressora)
- [ ] Autenticação na API (token)
- [ ] Testes automatizados + CI
- [ ] Suporte a imagem/logo no cupom

## Notas técnicas (pitfalls)

- **NUL em bash**: variáveis bash truncam `\x00` — os comandos ESC/POS com NUL devem ser
  guardados como texto de escapes e enviados com `printf '%b'`.
- **Corte**: `GS V 66` exige um byte `n` extra; `GS V 49` avança e corta sozinho.
- **Área de impressão**: 72mm fixos (576 dots) = 48 colunas na Fonte A. `GS W` não aumenta.
- **Udev**: o node `lp0` é da subsystem `usbmisc` (não `usb`) — regra udev precisa disso.
- **LXC vs VM**: em LXC unprivileged o seccomp bloqueia `mknod`/`mount` (USB = config manual
  no host + restart). Em VM o passthrough USB é nativo (`usb0: host=20d1:7008`) e o device
  volta sozinho quando a impressora religa.
