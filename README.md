# Elgin I9 Printer 🖨️

Serviço de impressão para a **impressora térmica Elgin I9** (ESC/POS via USB) do homelab — agora num **único binário Go estático**.

- **Binário `elgin-print`**: substitui a antiga CLI bash + API FastAPI (um arquivo, zero dependências no destino).
- **CLI**: subcomandos `print`, `serve`, `feed`, `cut`.
- **API REST**: mantém os endpoints antigos (`GET /`, `GET /health`, `POST /test`, `POST /print`) + novos (`POST /feed`, `POST /cut`).
- **Web UI**: editor visual de cupom embutido no binário (`embed.FS`), em `http://<vm>:8000/`.
- **📚 Documentação técnica completa**: [`docs/impressora-elgin-i9.md`](docs/impressora-elgin-i9.md) — comandos ESC/POS, a saga do corte, feed físico, DIP switches, beep e pitfalls.

## Compilando

```bash
make cross     # binário estático linux/amd64 (roda em Alpine/musl, ~5MB)
```

Ou manualmente:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o elgin-print .
```

Há um CI (`.github/workflows/build.yml`) que roda `go vet`, `go test` e gera o binário a cada push/PR.

## Como usar (CLI)

```bash
./elgin-print print                        # cupom de teste completo
./elgin-print print "texto"                # mensagem centralizada + moldura + corte
./elgin-print print -e "esq" -c "centro" -d "dir"   # multilinha com alinhamentos
./elgin-print print -t "Título" -c "texto"          # título 2x + corpo
./elgin-print feed 5                       # avança 5 linhas
./elgin-print cut                          # aciona a guilhotina
```

O corte é automático (`GS V 66 0`), enviado em write separado com delay para não furar o buffer. ✂️

## Como usar (API)

```bash
# documentação viva (endpoints, alinhamentos, fontes, preenchimento)
curl http://<host>:8000/

# health check (reflete presença/ausência real da impressora)
curl http://<host>:8000/health

# cupom de teste
curl -X POST http://<host>:8000/test

# avançar papel / cortar
curl -X POST http://<host>:8000/feed -H 'Content-Type: application/json' -d '{"linhas": 5}'
curl -X POST http://<host>:8000/cut

# cupom personalizado (alinhamento, fonte, negrito e padrão por linha)
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

> **Nota:** `GET /` serve a **Web UI** quando acessado por navegador (`Accept: text/html`)
> e a **documentação JSON** quando acessado por `curl`/API (`Accept: */*`) — preservando
> o comportamento antigo sem quebrar a UI no navegador.

**Campos por linha:**

- `texto` — até 48 caracteres (24 na fonte larga)
- `alinhamento` — `esquerda` | `centro` | `direita`
- `fonte` — `normal` | `larga` (largura 2x, bom para títulos/totais)
- `negrito` — `true`/`false`
- `padrao` — `true` repete o `texto` até preencher a linha (ex: `"-X"` vira `-X-X-X-X-...`)

## Web UI

Abra `http://<vm>:8000/` no navegador. Oferece:

- **Editor de cupom** (título + linhas com alinhamento/fonte/negrito/padrão) e pré-visualização 48 colunas.
- Botões **Imprimir** / **Feed N linhas** / **Corte** / **Cupom de teste**.
- **Layouts prontos** (teste, pedido, etiqueta, moldura).
- **Construtor de chamadas da API** (mostra o JSON e o `curl` prontos).

## Instalação na VM (Alpine)

A instalação é **copiar 1 binário + habilitar 1 serviço** (sem Python/venv/pip):

```bash
sudo ./deploy/setup.sh ./elgin-print
```

O `setup.sh` detecta o init system: **OpenRC** (Alpine) → `/etc/init.d/elgin-print`, ou **systemd** → `/etc/systemd/system/elgin-print.service`. Também instala a regra udev `50-elgin-i9.rules`.

Guia completo para criar a VM no Proxmox (com USB passthrough nativo e auto-reconexão): [`deploy/alpine-vm.md`](deploy/alpine-vm.md).

## 📥 Downloads (manuais e drivers)

Arquivos incluídos neste repositório:

- **Manuais** (`docs/manuais/`):
  - `manual-programacao-elgin-i9.pdf` — manual de programação (ESC/POS)
  - `manual-usuario-elgin-i9-full.pdf` — manual do usuário
  - `manual-rapido-elgin-i9.pdf` — guia rápido
- **Drivers Windows** (`drivers/`):
  - `Driver-Elgin-i9-FULL-1.8.0.2.exe` — driver i9 FULL (recomendado)
  - `Driver-Elgin-i9-1.8.0.1.exe` — driver i9

**Links oficiais:**

- Download completo (Windows + Linux + manuais, ~281MB):
  https://www.bztech.com.br/arquivos/driver-elgin-i7-i8-e-i9-windows-e-linux.zip
- Página de downloads (Bz Tech): https://www.bztech.com.br/downloads
- Manual de programação i9 (Bz Tech): https://www.bztech.com.br/downloads/manual-programacao-elgin-i9
- Wiki Elgin Developer Community (dicas, buzzer, logs):
  https://github.com/ElginDeveloperCommunity/Impressoras/wiki
- Elgin (fabricante): https://www.elgin.com.br/

## Roadmap

- [x] Script CLI de impressão (corte, alinhamento, fontes)
- [x] API REST básica (FastAPI)
- [x] Binário Go único (CLI + API + Web UI) — substitui bash + FastAPI
- [ ] VM dedicada com USB passthrough nativo (sem restart ao religar a impressora)
- [ ] Autenticação na API (token)
- [ ] Testes automatizados + CI
- [ ] Suporte a imagem/logo no cupom

## Notas técnicas (pitfalls)

- **NUL em bash**: variáveis bash truncam `\x00` — os comandos ESC/POS com NUL devem ser
  guardados como texto de escapes e enviados com `printf '%b'`. **No Go isso não existe**
  (bytes crus `[]byte`), o que simplifica o port.
- **Corte**: o comando correto é **`GS V 66 0`** (`1d 56 42 00`), que corta rente à última
  linha sem perder conteúdo. A i9 executa o `GS V` imediatamente ao receber (fura o buffer),
  então o corte vai num write separado, depois de um delay proporcional ao cupom
  (`max(1.5s, linhas * 0.2s)`). Os demais comandos (`GS V 0/1/49`) cortam ~2 linhas antes e
  misturam conteúdo entre cupons.
- **Feed no topo**: ~2-3 linhas de papel em branco no início de cada cupom é FÍSICO
  (distância guilhotina→cabeça da i9), não dá para remover por software. Detalhes em
  `docs/impressora-elgin-i9.md`.
- **Linhas vazias colapsam**: a i9 não avança papel em linha 100% vazia — o código envia um
  espaço invisível (`" "`) para forçar o avanço.
- **Área de impressão**: 72mm fixos (576 dots) = 48 colunas na Fonte A. `GS W` não aumenta.
- **Udev**: o node `lp0` é da subsystem `usbmisc` (não `usb`) — regra udev precisa disso.
- **LXC vs VM**: em LXC unprivileged o seccomp bloqueia `mknod`/`mount` (USB = config manual
  no host + restart). Em VM o passthrough USB é nativo (`usb0: host=20d1:7008`) e o device
  volta sozinho quando a impressora religa.
