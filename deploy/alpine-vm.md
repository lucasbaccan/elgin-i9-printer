# VM Alpine dedicada para a Elgin i9 (Proxmox)

Guia para criar a VM nova no Proxmox via API (sem SSH nos hosts) e deixá-la
rodando o binário `elgin-print` com USB passthrough nativo e auto-reconexão.

## Por que VM em vez de LXC?

Em LXC unprivileged o passthrough USB é manual (cgroup + bind mount no host) e
**religar a impressora derruba o `/dev/usb`** (inode "deleted"), exigindo restart
do container. Em **VM QEMU**, o passthrough é nativo (`usb0: host=...`): o QEMU
re-apega o device sozinho quando a impressora religa, e o udev recria o
`/dev/usb/lp0` automaticamente. Ver `docs/impressora-elgin-i9.md`.

## Pré-requisitos

- Token da API do Proxmox (cluster-wide). Ex.: `mordomo-bot@pam!mordomo-bot`.
- Um host online (pve1/pve2/pve3 — use qualquer um, o token é cluster-wide).
- ISO do Alpine Linux disponível num storage (ver abaixo).
- **Atenção:** a impressora hoje está no LXC 104. Criar a VM e passar o USB para
  ela vai **interromper a impressão no LXC 104** até a migração ser validada.

```bash
export PROXMOX_TOKEN="<user>@<realm>!<tokenid>=<secret>"
export PVE="https://192.168.50.10:8006/api2/json"   # pve1 (ou .20/.30)
AUTH="Authorization: PVEAPIToken=$PROXMOX_TOKEN"
```

## 1. Baixar o ISO do Alpine

```bash
# storage 'isos' (CIFS //192.168.50.50/ISOs) já montado nos 3 nós
curl -sk "$PVE/nodes/pve1/storage/isos/upload" \
  -H "$AUTH" \
  -F content=iso \
  -F filename=@/caminho/alpine-virt-3.20-x86_64.iso
```

Use a imagem **virt** (feita para VM). Download oficial:
<https://dl-cdn.alpinelinux.org/alpine/v3.20/releases/x86_64/alpine-virt-3.20.3-x86_64.iso>

## 2. Criar a VM (com USB passthrough)

Escolha um VMID livre e um storage com espaço. O `usb0` já passa a impressora:

```bash
curl -sk -X POST "$PVE/nodes/pve2/qemu" \
  -H "$AUTH" \
  --data-urlencode "vmid=110" \
  --data-urlencode "name=elgin-print" \
  --data-urlencode "memory=512" \
  --data-urlencode "cores=1" \
  --data-urlencode "sockets=1" \
  --data-urlencode "cpu=x86-64-v2-AES" \
  --data-urlencode "ostype=l26" \
  --data-urlencode "scsihw=virtio-scsi-single" \
  --data-urlencode "scsi0=local-btrfs:4" \
  --data-urlencode "net0=virtio,bridge=vmbr0" \
  --data-urlencode "ide2=isos:iso/alpine-virt-3.20.3-x86_64.iso,media=cdrom" \
  --data-urlencode "boot=order=ide2;scsi0" \
  --data-urlencode "usb0=host=20d1:7008" \
  --data-urlencode "agent=1" \
  --data-urlencode "tablet=1" \
  --data-urlencode "vga=std"
```

> **USB passthrough:** `usb0=host=20d1:7008` usa o identificador vendor:product.
> Para o device físico específico (recomendado se houver mais de um 20d1:7008),
> use `host=20d1:7008,usb3=0` ou o bus/port (`host=1-4`). Depois de criada, dá
> para ajustar: `POST .../qemu/110/config -d usb0=host=20d1:7008`.

## 3. Instalar o Alpine (console noVNC)

1. `POST .../qemu/110/status/start` para ligar.
2. Console noVNC → rodar `setup-alpine`, disco `sda`, boot `sys`.
3. Rede com **IP fixo** na 192.168.50.x (ex.: `192.168.50.51/24`, gw `192.168.50.1`).

## 4. Instalar o binário + serviço

Na VM Alpine (sem Python, sem dependências):

```sh
apk add curl           # só para o health-check de verificação (opcional)
# copie o binário estático e o deploy/ (scp de onde compilou)
sudo ./deploy/setup.sh ./elgin-print
```

`setup.sh` detecta OpenRC e instala `/etc/init.d/elgin-print` + regra udev. A
instalação toda é **copiar 1 binário + 1 serviço**.

## 5. Auto-reconexão (mecanismo e por quê)

A reconexão da impressora é resolvida em três camadas, sem nenhum daemon extra:

1. **QEMU usb-host**: quando a impressora é desligada/religada, o QEMU re-apega o
   device automaticamente (passthrough nativo de VM). Nada a re-armar.
2. **udev**: a regra `50-elgin-i9.rules` reaplica `MODE=0666` no `/dev/usb/lp0`
   assim que o node é recriado.
3. **binário Go**: cada operação abre o device fresco (`os.OpenFile` por write) e
   o `watchdog` interna loga as transições conectado/desconectado no journal; o
   `/health` faz `os.Stat` a cada request e reflete o estado REAL.

Ou seja: desligou a impressora → `lp0` some → `/health` reporta `ok:false`;
religou → QEMU re-apega → udev recria o node → próximo print/health abre o node
novo e funciona, sem reiniciar nada.

## 6. Validação

```sh
curl http://192.168.50.51:8000/health        # {"ok":true,...}
curl -X POST http://192.168.50.51:8000/test  # cupom de teste com corte
# desligue e religue a impressora, confira /health voltar a true, e imprima de novo
```
