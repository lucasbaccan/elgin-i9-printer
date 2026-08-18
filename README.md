# Elgin I9 Printer 🖨️

Serviço de impressão para a **impressora térmica Elgin I9** (ESC/POS via USB) — num **único binário Go estático**.

- **Binário `elgin-print`**: substitui a antiga CLI bash + API FastAPI (um arquivo, zero dependências no destino).
- **CLI**: subcomandos `print`, `serve`, `feed`, `cut`.
- **API REST**: mantém os endpoints antigos (`GET /`, `GET /health`, `POST /test`, `POST /print`) + novos (`POST /feed`, `POST /cut`, `GET /qr`).
- **Web UI**: editor visual de cupom embutido no binário (`embed.FS`), em `http://<vm>:8000/`.
- **📚 Documentação técnica completa**: [`docs/impressora-elgin-i9.md`](docs/impressora-elgin-i9.md) — comandos ESC/POS, a saga do corte, feed físico, DIP switches, beep e pitfalls.

## Compilando

O projeto usa [mise](https://mise.jdx.dev/) para a versão do Go (definida em
[`mise.local.toml`](mise.local.toml) — `go 1.26.6`):

```bash
mise install          # instala a versão do Go declarada
mise exec go --version
```

**Linux (binário estático — roda em Alpine/musl):**

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o elgin-print .
```

**Windows (exe):**

```powershell
$env:CGO_ENABLED="0"; $env:GOOS="windows"; $env:GOARCH="amd64"
go build -trimpath -ldflags "-s -w" -o elgin-print.exe .
```

**Testes e lint:**

```bash
go vet ./... && go test ./...
```

Há um CI (`.github/workflows/build.yml`) que roda `go vet`, `go test` e gera o binário a cada push/PR,
e outro (`.github/workflows/build-binaries.yml`) que gera os binários **Linux e Windows** em PRs.

### Observações

- **Impressora padrão (Windows)**: sem `ELGIN_LP`, o binário usa a **impressora padrão do Windows**. Se a padrão for
  "Microsoft Print to PDF", o job é salvo como arquivo em Documentos em vez de imprimir — defina a Elgin como padrão
  ou aponte `ELGIN_LP` para o nome da fila (ex.: `ELGIN_LP="ELGIN i9(USB)"`).
- **Detecção automática (Linux)**: sem `ELGIN_LP`, o binário acha a Elgin pelo USB ID `20d1:7008`; não achando, usa a
  primeira impressora USB (modo genérico) — veja a seção abaixo.
- **Windows + spooler**: o envio é via winspool (job RAW), então a fila precisa existir com o driver instalado
  (driver da Elgin ou "Generic / Text Only"). A porta da fila deve ser USB (não `FILE:`).
- **`ELGIN_LP`** aceita device no Linux (`/dev/usb/lpN`) ou nome da fila no Windows.

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
      {"texto": "-X-", "alinhamento": "esquerda", "linha": true},
      {"texto": "1x Hamburguer", "alinhamento": "esquerda"},
      {"texto": "TOTAL: R$ 45,00", "alinhamento": "direita", "fonte": "larga", "negrito": true},
      {"texto": "-X-", "alinhamento": "esquerda", "linha": true}
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
- `linha` — `true` repete o `texto` até preencher a linha (ex: `"-X"` vira `-X-X-X-X-...`)

**Blocos gráficos** (novo — imagem e QR code):

- `tipo` — `texto` (padrão/ausente) | `imagem` | `qr`
- `imagem` — base64 (PNG/JPEG/GIF, com ou sem prefixo `data:`); a imagem é convertida para 1-bit
  (dither Floyd-Steinberg) e ajustada à largura do papel (576 dots) mantendo a proporção
- `qr` — conteúdo do QR (URL/texto); gerado no servidor e impresso como imagem (fallback `GS v 0`,
  não depende do suporte a `GS ( k`)
- `qr_tamanho` — tamanho do módulo do QR `1..8` (padrão `4`); se não couber em 576 dots, o módulo é reduzido

Exemplo com imagem e QR:

```bash
curl -X POST http://<host>:8000/print \
  -H 'Content-Type: application/json' \
  -d '{
    "titulo": "CUPOM DIGITAL",
    "linhas": [
      {"texto": "Pague via PIX:", "alinhamento": "centro"},
      {"tipo": "qr", "qr": "https://exemplo.com/pix", "qr_tamanho": 4},
      {"tipo": "imagem", "imagem": "data:image/png;base64,iVBORw0KGgo..."}
    ]
  }'
```

Também há o endpoint `GET /qr?text=...&tamanho=1..8` que renderiza um QR como PNG
(sem imprimir) — útil para conferir antes de mandar para o papel.

## Web UI

Abra `http://<vm>:8000/` no navegador. Oferece:

- **Editor de cupom** (título + linhas com alinhamento/fonte/negrito/linha) e pré-visualização 48 colunas com **marcador do corte** (tesoura).
- **Blocos de lista** (bullet/checkbox) — cada item vira uma linha com marcador na impressão.
- **Blocos de imagem** (upload de arquivo ou Ctrl+V) e **QR code** (a partir de texto/URL, com tamanho de módulo 1–8) — ambos com preview.
- Botões **Imprimir** / **Feed N linhas** / **Corte** / **Ping**.
- **Layouts prontos** (teste, pedido, etiqueta, moldura).
- **Construtor de chamadas da API** (mostra o JSON e o `curl` prontos).

## Detecção da impressora (Linux)

Sem `ELGIN_LP`, o binário procura a **Elgin I9 pelo USB ID `20d1:7008`** no sysfs
(`/sys/class/usbmisc/lp*` / `/sys/class/usb/lp*`). **Não achando o ID, cai no
modo genérico**: usa a primeira impressora USB disponível (`/dev/usb/lp0`, `lp1`…).
Sem nenhuma impressora, mantém `/dev/usb/lp0` e o `/health` reporta indisponível.

## Windows

O binário compila para Windows (`GOOS=windows go build`) e envia os bytes
ESC/POS pela **fila de impressão via winspool (job RAW)** — funciona com o
driver da Elgin ou "Generic / Text Only". Sem `ELGIN_LP`, usa a **impressora
padrão do Windows** (modo genérico); com mais de uma térmica, defina
`ELGIN_LP` com o **nome da fila** (ex.: `ELGIN_LP=Elgin I9`). Não há detecção
por USB ID no Windows (exigiria SetupAPI).

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
- **Container vs VM**: em container (Docker/LXC) o sysfs é limitado — a detecção
  automática por USB ID não funciona, use `ELGIN_LP` explícito. Em VM com USB
  passthrough nativo (`hostdev`/`usb0: host=20d1:7008`) o device volta sozinho
  quando a impressora religa.
