#!/bin/bash
# ============================================================
#  Impressora termica Elgin I9 - LXC hermesagent
#  Acesso: /dev/usb/lp0 (usblp) ou /dev/bus/usb/001/0XX (libusb)
#  Uso:
#    ./imprimir_elgin.sh                      -> cupom de teste completo
#    ./imprimir_elgin.sh "texto"              -> imprime texto (centro) + moldura + corte
#    ./imprimir_elgin.sh -e "txt" -c "txt2" -d "txt3"  -> multilinha c/ alinhamentos
#    ./imprimir_elgin.sh -t "titulo" -e "txt" -c "txt2" -d "txt3"
# ============================================================
LP=/dev/usb/lp0

# --- comandos ESC/POS da Elgin i9 ---
# IMPORTANTE: guardar como TEXTO de escapes (NAO bytes reais) porque o bash
# trunca \x00 em variaveis. O printf %b do send() interpreta na saida.
INI='\x1b\x40'              # ESC @ - inicializa
FONTE_A='\x1b\x4d\x00'      # ESC M NUL - Fonte A 12x24 (48 colunas, area 72mm)
LARG2='\x1d\x21\x10'        # GS ! 16 - largura 2x
LARG1='\x1d\x21\x00'        # GS ! 0 - largura normal
ESQ='\x1b\x61\x00'          # ESC a NUL - alinhamento esquerda
CEN='\x1b\x61\x01'          # ESC a 1 - alinhamento centro
DIR='\x1b\x61\x02'          # ESC a 2 - alinhamento direita
CORTE='\x1d\x56\x31'        # GS V 49 - avanca ate o corte + corte automatico
MOLDURA='================================================'  # 48x (largura cheia)

# --- envia bytes para a impressora (%b interpreta \n, \x1b, \x00, etc.) ---
send() { printf '%b' "$1" > "$LP" || { echo "ERRO: nao conseguiu escrever em $LP"; exit 1; }; }

# --- moldura de ponta a ponta (48 colunas) ---
moldura() { send "$ESQ$MOLDURA\n"; }

# --- titulo em largura 2x (24 chars = largura cheia) ---
titulo() {
  local t="$1"
  send "$LARG2$CEN$t\n$LARG1"
}

# --- cupom de teste completo ---
teste() {
  send "$INI$FONTE_A"
  moldura
  titulo '*** MENSAGEM SECRETA ***'
  send "$ESQ"
  send 'CLASSIFICACAO: ULTRA SECRETA\n'
  send 'DESTINATARIO: SO VOCE\n'
  send '\n'
  send "$CEN"
  send 'PSST... NAO CONTA PRA NINGUEM:\n'
  send '\n'
  send 'O HERMES ACHA QUE VOCE E O\n'
  send 'MELHOR CHEFE DO UNIVERSO!\n'
  send '\n'
  send "$DIR"
  send 'SO QUE NAO, EU NAO DISSE NADA.\n'
  send 'ESSA MENSAGEM SE AUTO-DESTRUIU.\n'
  send 'ASSINADO: HERMES, O MORDOMO\n'
  send '\n'
  send "$CEN"
  moldura
  send 'FIM DA TRANSMISSAO\n'
  moldura
  send '\n\n\n'
  send "$CORTE"
  echo "OK - cupom de teste impresso!"
}

# --- mensagem personalizada (multilinha: -e/-c/-d antes de cada texto) ---
mensagem() {
  local tit="" linhas=() aligns=()
  while [ $# -gt 0 ]; do
    case "$1" in
      -e|-c|-d)
        local a="$1"; shift
        [ $# -gt 0 ] || { echo "ERRO: falta texto depois de $a"; exit 1; }
        linhas+=("$1"); aligns+=("$a"); shift ;;
      -t) tit="$2"; shift 2 ;;
      *) # texto solto sem flag: centralizado
        linhas+=("$1"); aligns+=("-c"); shift ;;
    esac
  done
  if [ ${#linhas[@]} -eq 0 ]; then
    linhas+=("MENSAGEM DO HERMES"); aligns+=("-c")
  fi
  send "$INI$FONTE_A"
  moldura
  [ -n "$tit" ] && titulo "$tit"
  for i in "${!linhas[@]}"; do
    local al="$CEN"
    case "${aligns[$i]}" in
      -e) al="$ESQ" ;;
      -d) al="$DIR" ;;
    esac
    send "$al${linhas[$i]}\n"
  done
  send '\n\n\n'
  send "$CEN$MOLDURA\n"
  send "$CORTE"
  echo "OK - ${#linhas[@]} linha(s) impressa(s)!"
}

# --- main ---
if [ ! -e "$LP" ]; then
  echo "ERRO: $LP nao existe. USB passthrough nao aplicado (restart do LXC resolve)."
  exit 1
fi
if [ ! -w "$LP" ]; then
  echo "ERRO: $LP sem permissao de escrita (udev rule 50-elgin-i9.rules no host)."
  exit 1
fi

if [ $# -eq 0 ]; then
  teste
else
  mensagem "$@"
fi
