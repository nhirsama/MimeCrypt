FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/mimecrypt ./cmd/mimecrypt

FROM alpine:3.22

RUN apk add --no-cache ca-certificates gnupg tini \
    && addgroup -S mimecrypt \
    && adduser -S -G mimecrypt -h /home/mimecrypt mimecrypt \
    && mkdir -p /state /backup /gnupg /tmp \
    && chown -R mimecrypt:mimecrypt /home/mimecrypt /state /backup /gnupg /tmp

COPY --from=build /out/mimecrypt /usr/local/bin/mimecrypt

ENV MIMECRYPT_STATE_DIR=/state \
    MIMECRYPT_BACKUP_DIR=/backup \
    MIMECRYPT_WORK_DIR=/tmp/mimecrypt \
    MIMECRYPT_AUDIT_STDOUT=false \
    MIMECRYPT_TOKEN_STORE=file \
    GNUPGHOME=/gnupg

USER mimecrypt
WORKDIR /home/mimecrypt

HEALTHCHECK --interval=30s --timeout=20s --start-period=20s --retries=3 CMD ["/usr/local/bin/mimecrypt", "health", "--timeout", "20s"]

ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/mimecrypt"]
CMD ["run"]
