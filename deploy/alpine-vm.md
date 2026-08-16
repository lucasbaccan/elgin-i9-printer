# VM dedicada para a Elgin i9 (Alpine + QEMU/KVM)

Guia para rodar o `elgin-print` numa VM Linux dedicada, com **USB passthrough
nativo** e auto-reconexão — útil quando a impressora é compartilhada com outros
sistemas ou você quer isolamento total. Os exemplos usam **libvirt/QEMU**
(comuns em qualquer hypervisor); adapte a forma de passar o USB ao seu.

## Por que VM em vez de rodar no host/container?

- Em **VM**, o passthrough USB é nativo: quando a impressora é desligada e
  religada, o hypervisor **re-apega o device sozinho** e o udev recria o
  `/dev/usb/lp0` — nada de restart.
- Em **container** (Docker/LXC) o sysfs é limitado: a detecção automática por
  USB ID não funciona (use `ELGIN_LP` explícito) e religar a impressora pode
  deixar o device "preso" — precisa reiniciar o container.

## Pré-requisitos

- ISO do Alpine Linux (**imagem virt**): <https://dl-cdn.alpinelinux.org/alpine/v3.20/releases/x86_64/alpine-virt-3.20.3-x86_64.iso>
- Um hypervisor QEMU/KVM com suporte a USB passthrough (libvirt, oVirt, Hyper-V…)

## 1. Criar a VM

Exemplo libvirt (XML) — máquina mínima (512MB RAM, 1 vCPU, 4GB disco) com a
impressora anexada por **vendor:product** (`20d1:7008` = Elgin I9):

```xml
<domain type='kvm'>
  <name>elgin-print</name>
  <memory unit='MiB'>512</memory>
  <vcpu>1</vcpu>
  <os><type arch='x86_64'>hvm</type></os>
  <devices>
    <disk type='file' device='disk'>
      <source file='/var/lib/libvirt/images/elgin-print.qcow2'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <interface type='network'>
      <source network='default'/>
      <model type='virtio'/>
    </interface>
    <hostdev mode='subsystem' type='usb'>
      <source>
        <vendor id='0x20d1'/>
        <product id='0x7008'/>
      </source>
    </hostdev>
  </devices>
</domain>
```

> **USB passthrough**: anexe por `vendor:product` (`20d1:7008`) para não
> depender de qual porta física a impressora está. Se houver mais de uma Elgin
> na mesma máquina, use o bus/port do device físico.

## 2. Instalar o Alpine

1. Ligue a VM com o ISO de instalação (CDROM) e abra o console.
2. Rode `setup-alpine`: disco `sda`, boot **`sys`** (instalado em disco).
3. Configure a rede (IP fixo ou DHCP) e um usuário com `sudo`.

## 3. Instalar o binário + serviço

Na VM Alpine (sem Python, sem dependências):

```sh
# copie o binário estático (scp de onde compilou) e instale como serviço:
sudo install -m 0755 elgin-print /usr/local/bin/elgin-print

# Alpine (OpenRC):
sudo cp deploy/elgin-print.initd /etc/init.d/elgin-print
sudo rc-update add elgin-print default
sudo rc-service elgin-print start

# Demais distros (systemd):
sudo cp deploy/elgin-print.service /etc/systemd/system/
sudo systemctl enable --now elgin-print

# regra udev de permissão (0666 no /dev/usb/lp0):
sudo cp deploy/50-elgin-i9.rules /etc/udev/rules.d/
sudo udevadm control --reload
```

A detecção automática por USB ID funciona numa VM (sysfs completo) — o serviço
não precisa de `ELGIN_LP` configurado.

## 4. Auto-reconexão (mecanismo e por quê)

Três camadas, sem daemon extra:

1. **Hypervisor (QEMU usb-host)**: religou a impressora → re-apega o device
   automaticamente (passthrough nativo de VM). Nada a re-armar.
2. **udev**: a regra `50-elgin-i9.rules` reaplica `MODE=0666` no `/dev/usb/lp0`
   assim que o node é recriado.
3. **Binário Go**: cada operação abre o device fresco e o `/health` faz
   `os.Stat` a cada request — reflete o estado REAL a qualquer momento.

Ou seja: desligou a impressora → `lp0` some → `/health` reporta `ok:false`;
religou → hypervisor re-apega → udev recria o node → o próximo print abre o
node novo e funciona, **sem reiniciar nada**.

## 5. Validação

```sh
curl http://<ip-da-vm>:8000/health        # {"ok":true,...}
curl -X POST http://<ip-da-vm>:8000/test  # cupom de teste com corte
# desligue e religue a impressora, confira /health voltar a true, e imprima de novo
```
