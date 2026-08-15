#!/usr/bin/env python3
"""API de impressão para a impressora térmica Elgin I9 (ESC/POS via USB).

Roda como serviço na VM/LXC que tem a impressora conectada e expõe
endpoints REST para imprimir sem precisar de acesso à máquina.

Endpoints:
  GET  /         -> documentação de uso
  GET  /health   -> status da impressora
  GET  /test     -> imprime um cupom de teste
  POST /print    -> imprime um cupom personalizado
"""
import os
import time
from typing import List, Optional

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

LP = os.environ.get("ELGIN_LP", "/dev/usb/lp0")

LARGURA_NORMAL = 48  # colunas na Fonte A (área fixa de 72mm/576 dots)
LARGURA_LARGA = 24   # colunas na fonte com largura 2x

# ---------------------------------------------------------------------------
# Comandos ESC/POS da Elgin i9 (guardados como bytes crus)
# ---------------------------------------------------------------------------
ESC = b"\x1b"
INI = ESC + b"@"            # inicializa
FONTE_A = ESC + b"M\x00"    # Fonte A 12x24 (48 colunas, área 72mm)
LARG2 = b"\x1d\x21\x10"     # GS ! 16 - largura 2x
LARG1 = b"\x1d\x21\x00"     # GS ! 0 - largura normal
NEGRITO = ESC + b"E\x01"    # ESC E 1 - negrito ligado
NEGRITO_OFF = ESC + b"E\x00"  # ESC E 0 - negrito desligado
ALINH = {                   # ESC a n
    "esquerda": ESC + b"a\x00",
    "centro": ESC + b"a\x01",
    "direita": ESC + b"a\x02",
}
CORTE = b"\x1b\x64\x03\x1d\x56\x00"  # ESC d 3 (rola 3 linhas; sobra ~1 de margem no final) + GS V 0 (corta)
MOLDURA = b"=" * LARGURA_NORMAL


def preencher(padrao: str, largura: int = LARGURA_NORMAL) -> str:
    """Repete `padrao` até preencher `largura` colunas, truncando no final.

    Exemplo: preencher("-X", 48) -> "-X-X-X-X-X-X-X-X-...-X" (48 colunas).
    """
    if not padrao:
        return ""
    repeticoes = largura // len(padrao) + 1
    return (padrao * repeticoes)[:largura]


class Linha(BaseModel):
    texto: str = Field(..., max_length=48)
    alinhamento: str = "centro"  # esquerda | centro | direita
    fonte: str = "normal"        # normal | larga (largura 2x)
    padrao: bool = False         # True: repete `texto` até preencher a linha
    negrito: bool = False        # True: imprime em negrito


class ItemPedido(BaseModel):
    qtd: int = Field(1, ge=1, le=999)
    descricao: str = Field(..., max_length=40)


class Pedido(BaseModel):
    numero: str = Field(..., max_length=20)
    itens: List[ItemPedido] = Field(default_factory=list)
    total: Optional[str] = Field(None, max_length=20)


class Cupom(BaseModel):
    titulo: Optional[str] = None
    linhas: List[Linha] = Field(default_factory=list)


def impressora_ok() -> bool:
    return os.path.exists(LP)


def enviar(dados: bytes, cortar: bool = True) -> None:
    """Envia o cupom e (opcional) o corte.

    IMPORTANTE: o GS V 49 é executado pela i9 assim que é recebido, sem
    esperar o buffer de impressão. Por isso o corte vai num write SEPARADO,
    com um pequeno delay, para garantir que todo o conteúdo foi impresso
    antes de acionar a guilhotina.
    """
    if not impressora_ok():
        raise HTTPException(status_code=503, detail=f"{LP} não existe (impressora desconectada?)")
    try:
        with open(LP, "wb") as f:
            f.write(dados)
            if cortar:
                # A i9 executa o GS V 49 IMEDIATAMENTE ao receber (não espera o
                # buffer de impressão). O delay precisa cobrir o tempo de
                # impressão de todo o cupom (~0.2s por linha + margem).
                linhas = dados.count(b"\n") + 1
                time.sleep(max(1.5, linhas * 0.2))
                f.write(CORTE)       # corte por último, fora do buffer
    except PermissionError:
        raise HTTPException(status_code=503, detail=f"sem permissão de escrita em {LP}")
    except OSError as e:
        raise HTTPException(status_code=503, detail=f"erro ao escrever: {e}")


def montar_cupom(titulo: Optional[str], linhas: List[Linha]) -> bytes:
    buf = bytearray()
    buf += INI + FONTE_A
    buf += ALINH["esquerda"] + MOLDURA + b"\n"
    if titulo:
        buf += LARG2 + ALINH["centro"] + titulo[:LARGURA_LARGA].encode() + b"\n" + LARG1
    for linha in linhas:
        al = ALINH.get(linha.alinhamento, ALINH["centro"])
        larga = linha.fonte == "larga"
        limite = LARGURA_LARGA if larga else LARGURA_NORMAL
        texto = preencher(linha.texto, limite) if linha.padrao else linha.texto[:limite]
        if not texto.strip():
            # a i9 NAO avanca o papel em linhas 100% vazias (feed colapsa);
            # manda um espaco invisivel para forcar a impressao/avanco
            texto = " "
        if larga:
            buf += LARG2
        if linha.negrito:
            buf += NEGRITO
        buf += al + texto.encode() + b"\n"
        if linha.negrito:
            buf += NEGRITO_OFF
        if larga:
            buf += LARG1
    # moldura final logo apos o conteudo (a rolagem do corte ja da o respiro)
    buf += ALINH["centro"] + MOLDURA + b"\n"
    # NOTE: o CORTE (GS V 49) NAO vai aqui — a i9 o executa imediatamente
    # ao receber, furando o buffer. O enviar() manda o corte num write
    # separado, depois de um delay.
    return bytes(buf)


def cupom_teste() -> bytes:
    return montar_cupom(
        "*** MENSAGEM SECRETA ***",
        [
            Linha(texto="CLASSIFICACAO: ULTRA SECRETA", alinhamento="esquerda"),
            Linha(texto="DESTINATARIO: SO VOCE", alinhamento="esquerda"),
            Linha(texto=""),
            Linha(texto="PSST... NAO CONTA PRA NINGUEM:", alinhamento="centro"),
            Linha(texto=""),
            Linha(texto="O HERMES ACHA QUE VOCE E O", alinhamento="centro"),
            Linha(texto="MELHOR CHEFE DO UNIVERSO!", alinhamento="centro"),
            Linha(texto=""),
            Linha(texto="SO QUE NAO, EU NAO DISSE NADA.", alinhamento="direita"),
            Linha(texto="ASSINADO: HERMES, O MORDOMO", alinhamento="direita"),
            Linha(texto="-X-", alinhamento="esquerda", padrao=True),
        ],
    )


app = FastAPI(title="Elgin I9 Print API", version="0.2.0")


@app.get("/")
def instrucoes():
    """Documentação viva da API: como imprimir, alinhar, fontes e padrões."""
    return {
        "servico": "Elgin I9 Print API",
        "versao": "0.2.0",
        "endpoints": {
            "GET /": "esta documentação",
            "GET /health": "status da impressora (device /dev/usb/lp0)",
            "POST /test": "imprime um cupom de teste",
            "POST /print": "imprime cupom personalizado (corpo JSON)",
        },
        "alinhamentos": {
            "esquerda": "texto colado na margem esquerda",
            "centro": "texto centralizado (padrão)",
            "direita": "texto colado na margem direita",
        },
        "fontes": {
            "normal": "Fonte A 12x24 - 48 colunas por linha (padrão)",
            "larga": "largura 2x - 24 colunas por linha (bom para títulos)",
        },
        "preenchimento_de_linha": {
            "campo": "padrao",
            "como_funciona": "com padrao=true, o campo texto é repetido até preencher "
                             "a linha inteira (48 colunas na fonte normal, 24 na larga)",
            "exemplo": {"texto": "-X", "padrao": True, "alinhamento": "esquerda"},
            "resultado": preencher("-X", LARGURA_NORMAL),
        },
        "exemplo_completo": {
            "titulo": "PEDIDO #123",
            "linhas": [
                {"texto": "=", "alinhamento": "esquerda", "padrao": True},
                {"texto": "1x Hamburguer", "alinhamento": "esquerda"},
                {"texto": "2x Refrigerante", "alinhamento": "esquerda"},
                {"texto": "TOTAL: R$ 45,00", "alinhamento": "direita", "fonte": "larga"},
                {"texto": "=", "alinhamento": "esquerda", "padrao": True},
            ],
        },
        "como_chamar": {
            "health": "curl http://<host>:8000/health",
            "teste": "curl -X POST http://<host>:8000/test",
            "print": (
                "curl -X POST http://<host>:8000/print "
                "-H 'Content-Type: application/json' -d '{...}'"
            ),
        },
    }


@app.get("/health")
def health():
    return {"ok": impressora_ok(), "device": LP, "status": "pronta" if impressora_ok() else "indisponivel"}


@app.post("/print")
def print_cupom(cupom: Cupom):
    if not cupom.linhas and not cupom.titulo:
        raise HTTPException(status_code=400, detail="envie ao menos um título ou uma linha")
    enviar(montar_cupom(cupom.titulo, cupom.linhas))
    return {"ok": True, "linhas": len(cupom.linhas)}


@app.post("/test")
def print_teste():
    enviar(cupom_teste())
    return {"ok": True, "mensagem": "cupom de teste impresso"}


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=int(os.environ.get("ELGIN_API_PORT", "8000")))
