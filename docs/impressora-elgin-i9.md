# Impressora Térmica Elgin i9 — Documentação Técnica Completa

Tudo que foi descoberto na prática (testando na impressora física), incluindo o que o manual não conta.

## Identificação

- **Fabricante/Modelo**: Elgin i9 (também conhecida como Bematech i9 Full)
- **USB**: `20d1:7008` (vendor:product)
- **Serial (deste equipamento)**: `17113221`
- **Interface**: USB Full Speed (12 Mbps), driver `usblp`
- **Comando**: ESC/POS
- **Corte**: guilhotina PARCIAL (deixa 1 ponto segurando o papel — não existe corte total)
- **ieee1284_id**: `MANUFACTURER:;COMMAND SET:ESC/POS;MODEL:ELGIN I9;COMMENT:Impact Printer;ACTIVE COMMAND:ESC/POS;`

## Acesso ao dispositivo

| Path | Uso | Observações |
|---|---|---|
| `/dev/usb/lp0` | Escrita ESC/POS direta (usblp, major 180) | **Some se a impressora for religada** com o LXC rodando (o udev do host remove/recria `/dev/usb`; o bind mount fica preso no inode "deleted"). Solução: reiniciar o LXC |
| `/dev/bus/usb/001/0XX` | Device inteiro via libusb (major 189) | Diretório estável; o node muda de número a cada religada (`015` → `020`) |

## Área de impressão e fontes

- **Área de impressão: 72mm fixos (576 dots)** — `GS W` NÃO aumenta. Papel de 80mm tem ~4mm de margem morta de cada lado.
- **Fonte A (12x24)**: 48 colunas por linha (padrão)
- **Fonte B (9x17)**: 64 colunas (menor)
- **Fonte "larga" (largura 2x)**: 24 colunas — bom para títulos e totais
- **Negrito**: `ESC E 1` / `ESC E 0`

## Comandos ESC/POS essenciais

| Função | Bytes | Notas |
|---|---|---|
| Inicializar | `ESC @` (`1b 40`) | |
| Fonte A | `ESC M 0` (`1b 4d 00`) | |
| Largura 2x | `GS ! 16` (`1d 21 10`) | |
| Largura normal | `GS ! 0` (`1d 21 00`) | |
| Alinhar esquerda | `ESC a 0` (`1b 61 00`) | |
| Centralizar | `ESC a 1` (`1b 61 01`) | |
| Alinhar direita | `ESC a 2` (`1b 61 02`) | |
| Negrito | `ESC E 1` / `ESC E 0` | |
| Avançar n linhas | `ESC d n` | |

## ⚠️ CORTE — a saga completa (testado na prática)

### O comando correto

```python
CORTE = b"\x1d\x56\x42\x00"  # GS V 66 n=0 — "completa até a posição de corte + corta"
```

**Corta rente à última linha impressa, sem perder conteúdo.** Enviar em write SEPARADO, com delay proporcional ao cupom (~0.2s/linha, mínimo 1.5s), porque:

### Descoberta 1: a i9 executa o `GS V` IMEDIATAMENTE ao receber

O comando de corte **fura o buffer de impressão** — ele é executado na hora, antes do restante do conteúdo ainda não impresso. Sintoma: o corte acontecia no MEIO do cupom (a moldura final ficava "abaixo do corte" e aparecia grudada no próximo cupom).

**Fix**: enviar o conteúdo primeiro, aguardar o tempo de impressão (delay), e só então enviar o `GS V` num write separado.

### Descoberta 2: os comandos de corte testados e seus comportamentos

| Comando | Bytes | Comportamento observado |
|---|---|---|
| `GS V 0` | `1d 56 00` | Corta na posição atual = **~2 linhas ANTES da última linha impressa** — ROUBOU o separador + moldura final (conteúdo cai no pedaço de baixo, misturando cupons) |
| `GS V 1` / `GS V 49` | `1d 56 01/31` | "Alimenta até a posição de corte" — na prática cortou ~2 linhas antes também (textos misturados entre pedaços) |
| `GS V 66 n=0` | `1d 56 42 00` | **Corta rente à última linha** — o correto ✅ |
| `GS V 66 n=50` | `1d 56 42 32` | Avança 50 unidades extras antes de cortar — feed grande |
| `ESC d 3` + `GS V 0` | `1b 64 03 1d 56 00` | Corta 1 linha após a última (funcionava, mas o `GS V 66 0` é mais limpo) |

### Descoberta 3: o feed no topo é FÍSICO (inevitável)

Entre um cupom e o próximo há **~2-3 linhas de papel em branco** — é a **distância entre a guilhotina e a cabeça de impressão**:

```
[corte do cupom anterior]   ← borda cortada fica na guilhotina
[2-3 linhas em branco]      ← distância física guilhotina → cabeça (FIXA)
[1ª linha do novo cupom]    ← impressa na cabeça
```

**Nenhum comando muda isso** (testamos: `GS V 0/1/49/66`, `ESC d`, cortes no início do cupom, remoção do `ESC @`). O `GS V 0` "parecia" não ter feed porque ele cortava 2 linhas antes — o conteúdo roubado do cupom anterior preenchia o espaço (por isso os textos apareciam misturados entre pedaços, ex: "B1 2 com o texto C0 1").

### Descoberta 4: linhas 100% vazias não avançam o papel

A i9 **colapsa feeds de linhas completamente vazias** (`\n\n\n` não avança). Sintoma: conteúdo empilhado. **Fix**: mandar um espaço `" "` em cada linha "vazia" — invisível, mas faz o papel avançar.

## DIP Switches (configuração física)

Localização: embaixo da impressora, atrás de uma tampinha (chave de fenda pequena). **Sempre com a impressora DESLIGADA.**

### DIP Switch 1 (SW1) — comunicação serial

| Chave | Função | ON | OFF | Padrão |
|---|---|---|---|---|
| 1-1 | Avanço Auto Line | Habilitado | Desabilitado | OFF |
| 1-2 | Handshaking | XON/XOFF | DTR/DSR | OFF |
| 1-3 | Bits de dados | 7 bits | 8 bits | OFF |
| 1-4 | Checar paridade | Sim | Não | OFF |
| 1-5 | Seleção de paridade | Par | Ímpar | OFF |
| 1-6/7/8 | Baud rate | Tabela A | | 38400 (OFF-ON-ON) |

Tabela A: 2400=ON-OFF-OFF · 4800=ON-OFF-ON · 9600=OFF-ON-OFF · 19200=OFF-OFF-OFF · **38400=OFF-ON-ON** · 57600=OFF-OFF-ON · 115200=ON-ON-ON

### DIP Switch 2 (SW2) — comportamento

| Chave | Função | ON | OFF | Padrão |
|---|---|---|---|---|
| 2-1 | Idioma | Português | Inglês | ON |
| 2-2 | Corte ao apertar AVANÇO | Habilitado | Desabilitado | OFF |
| 2-3 | **Cutter** | **Desabilitado** | **Habilitado** | OFF |
| 2-4 | Condição "Ocupada" | Offline | Recebe buffer completo | OFF |
| 2-5/6 | Densidade | Tabela B | | 1 (claro) |
| 2-7 | Botão AVANÇO | Imprime senha (UGS) | Avanço normal | OFF |
| 2-8 | Sensor de pouco papel | Desabilitado | Habilitado | OFF |

Tabela B: 1-Claro=ON-ON · 2=OFF-OFF · 3=ON-OFF · 4-Escuro=OFF-ON

**AutoTeste**: com a impressora desligada, segurar o botão AVANÇO e ligar — imprime as configurações atuais.

## Beep

- **Beep ao fim da impressão (campainha)**: configurado no DRIVER (Windows: Propriedades → Preferências → Configurações Avançadas → aba "Campainha"). Não é da impressora.
- **Bipes de erro** (tampa aberta: curto-longo-curto; sem papel: 3 curtos): **fixos no firmware**, não desabilitáveis.

## USB passthrough em LXC Proxmox (o caminho das pedras)

1. **LXC unprivileged bloqueia `mknod` e `mount` via seccomp** (EPERM mesmo como root — `grep Seccomp /proc/self/status` → 2). Não existe `usb0` no schema da API do LXC (só VM tem).
2. **Config manual no host** (`/etc/pve/lxc/<vmid>.conf`, backup antes):
   ```
   lxc.cgroup2.devices.allow: c 189:* rwm
   lxc.cgroup2.devices.allow: c 180:* rwm
   lxc.mount.entry: /dev/bus/usb dev/bus/usb none bind,create=dir,optional 0 0
   lxc.mount.entry: /dev/usb dev/usb none bind,create=dir,optional 0 0
   ```
   Requer **restart do LXC** para aplicar.
3. **Permissões**: os nodes nascem `root:lp 0660` e o root do container NÃO consegue chmod (uid não mapeado). Regra udev no HOST (`/etc/udev/rules.d/50-elgin-i9.rules`):
   ```
   SUBSYSTEM=="usb", ATTRS{idVendor}=="20d1", ATTRS{idProduct}=="7008", MODE="0666"
   SUBSYSTEM=="usbmisc", KERNEL=="lp*", ATTRS{idVendor}=="20d1", ATTRS{idProduct}=="7008", MODE="0666"
   ```
   ⚠️ O node `lp0` é da subsystem **`usbmisc`** (não `usb`) — regra com `SUBSYSTEM=="usb"` + `KERNEL=="lp*"` NÃO casa.

## Pitfalls de programação

- **NUL `\x00` em bash**: variáveis bash TRUNCAM NULs (`ESQ="$ESC"a$'\x00'` vira `ESC a`!). Sintoma: "a" impresso antes do texto, alinhamentos errados, texto "em negrito" (comandos desalinhados). **Fix**: guardar comandos como TEXTO de escapes (`ESQ='\x1b\x61\x00'`) e enviar com `printf '%b'` direto ao device (sem `$(...)`, que também engole NULs).
- **Delay antes do corte**: o `GS V` fura o buffer — sempre enviar em write separado com delay ≥ 1.5s ou ~0.2s/linha.
- **Linhas vazias**: usar `" "` em vez de `""`.
- **Espaço no topo**: físico, aceitar.

## Implementação atual (binário Go)

A impressão agora é feita por um **binário Go estático único** (`elgin-print`), que
substituiu a CLI bash + API FastAPI. Subcomandos: `print`, `serve` (API + Web UI),
`feed`, `cut`. Web UI embutida via `embed.FS` em `http://<vm>:8000/`.

- **Por que Go**: bytes crus `[]byte` nativos — sem o problema do NUL do bash, sem
  Python/venv/pip no destino, deploy = copiar 1 binário + 1 serviço.
- **Port dos quirks**: todos os comportamentos desta página (corte `GS V 66 0` em write
  separado com delay, linha vazia → `" "`, moldura 48 colunas, título 2x, `ESC @` no início)
  estão portados em `montarCupom`/`enviar` no código Go — nada foi redescoberto.
- **Endpoints**: `GET /` (Web UI no navegador / docs JSON via curl), `GET /health`,
  `POST /test`, `POST /print`, `POST /feed`, `POST /cut`. JSON compatível com a API antiga
  (`titulo` + `linhas[{texto, alinhamento, fonte, negrito, linha}]`).
- **Env**: `ELGIN_LP` (default `/dev/usb/lp0`), `ELGIN_API_PORT` (default `8000`).
- **Deploy**: `deploy/setup.sh` (OpenRC no Alpine, systemd nos demais) + regra udev
  `deploy/50-elgin-i9.rules`. Guia da VM: `deploy/alpine-vm.md`.

## Recomendação de arquitetura

**VM em vez de LXC**: o USB passthrough em VM é nativo (`usb0: host=20d1:7008`), o udev funciona, e religar a impressora não derruba nada. No LXC, religar a impressora exige reiniciar o container (o `/dev/usb` "deleted").

### Auto-reconexão em VM

Desligar/religar a impressora volta sozinho em três camadas, sem daemon extra:

1. **QEMU usb-host** re-apega o device automaticamente (passthrough nativo de VM).
2. **udev** reaplica `MODE=0666` no `/dev/usb/lp0` assim que o node é recriado.
3. **binário Go** abre o device fresco a cada operação e o `/health` faz `os.Stat` a cada
   request (estado real); um watchdog loga as transições no journal.

Ou seja: desligou → `lp0` some → `/health` reporta `ok:false`; religou → QEMU re-apega →
udev recria o node → próximo print/health funciona sem reiniciar nada.
