# elgin-print — imagem mínima (scratch): binário Go estático, zero dependências.
#
# Build:  docker build -t elgin-print .
# Rodar:  docker run --rm --device /dev/usb/lp0:/dev/usb/lp0 -p 8000:8000 elgin-print
#
# IMPORTANTE: dentro do container o sysfs é limitado — a detecção automática
# por USB ID NÃO funciona. Use ELGIN_LP explicitamente.

# ---- build ----
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/elgin-print . && mkdir -p /out/data

# ---- runtime ----
FROM scratch
COPY --from=build /out/elgin-print /elgin-print
# pasta de dados (imagens compartilhadas, 7 dias): monte um volume em /data
COPY --from=build /out/data /data
VOLUME /data
EXPOSE 8000
ENV ELGIN_LP=/dev/usb/lp0 \
    ELGIN_API_PORT=8000 \
    ELGIN_DATA_DIR=/data
ENTRYPOINT ["/elgin-print"]
CMD ["serve"]
