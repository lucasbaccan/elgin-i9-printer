#!/bin/sh
# Instala o binário elgin-print + serviço.
#   OpenRC (Alpine)  -> /etc/init.d/elgin-print
#   systemd (Debian) -> /etc/systemd/system/elgin-print.service
# Uso: sudo ./deploy/setup.sh [caminho/do/elgin-print]
set -e

BIN_SRC="${1:-./elgin-print}"

if [ ! -f "$BIN_SRC" ]; then
  echo "ERRO: binário não encontrado em $BIN_SRC"
  echo "Compile primeiro:  make cross   (ou ./deploy/build.sh)"
  exit 1
fi

echo "==> Instalando binário em /usr/local/bin/elgin-print"
install -m 0755 "$BIN_SRC" /usr/local/bin/elgin-print

# Regra udev: garante permissão 0666 no node lp* da impressora.
if [ -d /etc/udev/rules.d ]; then
  echo "==> Instalando regra udev 50-elgin-i9.rules"
  install -m 0644 "$(dirname "$0")/50-elgin-i9.rules" /etc/udev/rules.d/50-elgin-i9.rules
  udevadm control --reload-rules 2>/dev/null || true
  udevadm trigger 2>/dev/null || true
fi

if command -v rc-update >/dev/null 2>&1; then
  echo "==> OpenRC (Alpine): instalando serviço"
  install -m 0755 "$(dirname "$0")/elgin-print.initd" /etc/init.d/elgin-print
  rc-update add elgin-print default
  rc-service elgin-print restart
elif command -v systemctl >/dev/null 2>&1; then
  echo "==> systemd: instalando serviço"
  install -m 0644 "$(dirname "$0")/elgin-print.service" /etc/systemd/system/elgin-print.service
  systemctl daemon-reload
  systemctl enable --now elgin-print
else
  echo "AVISO: init system desconhecido — rode '/usr/local/bin/elgin-print serve' manualmente."
fi

echo "==> Verificando"
sleep 1
curl -s http://localhost:8000/health || true
echo
echo "Pronto! Web UI em http://<vm>:8000/  (endpoints: /health /print /test /feed /cut)"
