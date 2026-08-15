#!/usr/bin/env python3
"""API de impressão para a impressora térmica Elgin I9 (ESC/POS via USB).

Roda como serviço na VM/LXC que tem a impressora conectada e expõe
endpoints REST para imprimir sem precisar de acesso à máquina.

Endpoints:
  GET  /health  -> status da impressora
  GET  /test    -> imprime um cupom de teste
  POST /print   -> imprime um cupom personalizado
"""
import os
from typing import List, Optional

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

LP = os.environ.get("ELGIN_LP", "/dev/usb/lp0")

# ---------------------------------------------------------------------------
# Comandos ESC/POS da Elgin i9 (guardados como bytes crus)
# ---------------------------------------------------------------------------
ESC = b"\x1b"
INI = ESC + b"@"            # inicializa
FONTE_A = ESC + b"M\x00"    # Fonte A 12x24 (48 colunas, área 72mm)
LARG2 = b"\x1d\x21\x10"     # GS ! 16 - largura 2x
LARG1 = b"\x1d\x21\x00"     # GS ! 0 - largura normal
ALINH = {                   # ESC a n
    "esquerda": ESC + b"a\x00",
    "centro": ESC + b"a\x01",
    "direita": ESC + b"a\x02",
}
CORTE = b"\x1d\x56\x31"     # GS V 49 - avança até o corte + corta
MOLDURA = b"=" * 48


class Linha(BaseModel):
    texto: str = Field(..., max_length=48)
    alinhamento: str = "centro"  # esquerda | centro | direita


class Cupom(BaseModel):
    titulo: Optional[str] = None
    linhas: List[Linha] = Field(default_factory=list)


def impressora_ok() -> bool:
    return os.path.exists(LP)


def enviar(dados: bytes) -> None:
    if not impressora_ok():
        raise HTTPException(status_code=503, detail=f"{LP} não existe (impressora desconectada?)")
    try:
        with open(LP, "wb") as f:
            f.write(dados)
    except PermissionError:
        raise HTTPException(status_code=503, detail=f"sem permissão de escrita em {LP}")
    except OSError as e:
        raise HTTPException(status_code=503, detail=f"erro ao escrever: {e}")


def montar_cupom(titulo: Optional[str], linhas: List[Linha]) -> bytes:
    buf = bytearray()
    buf += INI + FONTE_A
    buf += ALINH["esquerda"] + MOLDURA + b"\n"
    if titulo:
        buf += LARG2 + ALINH["centro"] + titulo[:24].encode() + b"\n" + LARG1
    for linha in linhas:
        al = ALINH.get(linha.alinhamento, ALINH["centro"])
        buf += al + linha.texto[:48].encode() + b"\n"
    buf += b"\n\n\n"
    buf += ALINH["centro"] + MOLDURA + b"\n"
    buf += CORTE
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
        ],
    )


app = FastAPI(title="Elgin I9 Print API", version="0.1.0")


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
