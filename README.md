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
# documentação viva (endpoints, alinhamentos, fontes, preenchimento)
curl http://<host>:8000/

# health check
curl http://<host>:8000/health

# cupom de teste
curl -X POST http://<host>:8000/test

# cupom personalizado (alinhamento, fonte e padrão de preenchimento por linha)
curl -X POST http://<host>:8000/print \
  -H 'Content-Type: application/json' \
  -d '{
    "titulo": "PEDIDO #123",
    "linhas": [
      {"texto": "-X-", "alinhamento": "esquerda", "padrao": true},
      {"texto": "1x Hamburguer", "alinhamento": "esquerda"},
      {"texto": "TOTAL: R$ 45,00", "alinhamento": "direita", "fonte": "larga", "negrito": true},
      {"texto": "-X-", "alinhamento": "esquerda", "padrao": true}
    ]
  }'
```

**Campos por linha:**
- `texto` — até 48 caracteres (24 na fonte larga)
- `alinhamento` — `esquerda` | `centro` | `direita`
- `fonte` — `normal` | `larga` (largura 2x, bom para títulos/totais)
- `negrito` — `true`/`false`
- `padrao` — `true` repete o `texto` até preencher a linha (ex: `"-X"` vira `-X-X-X-X-...`)

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
- **Corte**: a i9 executa o `GS V` imediatamente ao receber (fura o buffer) e a guilhotina
  fica ~2 linhas acima da cabeça. O corte correto é **`ESC d 3` (rola) + `GS V 0` (corta)**
  num write separado, depois de um delay proporcional ao cupom (~0.2s/linha).
- **Linhas vazias**: a i9 não avança o papel em linhas 100% vazias — mandar um espaço `" "`.
- **Espaço no topo**: ~2-3 linhas de papel em branco no início de cada cupom é físico
  (distância guilhotina→cabeça da i9), não dá para remover por software.
- **Área de impressão**: 72mm fixos (576 dots) = 48 colunas na Fonte A. `GS W` não aumenta.
- **Udev**: o node `lp0` é da subsystem `usbmisc` (não `usb`) — regra udev precisa disso.
- **LXC vs VM**: em LXC unprivileged o seccomp bloqueia `mknod`/`mount` (USB = config manual
  no host + restart). Em VM o passthrough USB é nativo (`usb0: host=20d1:7008`) e o device
  volta sozinho quando a impressora religa.
