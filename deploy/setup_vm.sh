#!/bin/bash
# Setup da API de impressao Elgin I9 numa VM/LXC Debian/Ubuntu.
# Uso: sudo ./deploy/setup_vm.sh
set -e

echo "==> Instalando dependencias do sistema"
apt-get update
apt-get install -y python3 python3-venv python3-pip

echo "==> Instalando o projeto em /opt/elgin-i9-printer"
mkdir -p /opt/elgin-i9-printer
cp -r api /opt/elgin-i9-printer/
cp imprimir_elgin.sh /opt/elgin-i9-printer/

echo "==> Criando venv e instalando pacotes Python"
python3 -m venv /opt/elgin-i9-printer/venv
/opt/elgin-i9-printer/venv/bin/pip install -r /opt/elgin-i9-printer/api/requirements.txt

echo "==> Instalando servico systemd"
cp deploy/elgin-print-api.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now elgin-print-api

echo "==> Verificando"
systemctl status elgin-print-api --no-pager | head -5
curl -s http://localhost:8000/health
echo
echo "API no ar! Endpoints: GET /health, POST /print, POST /test"
