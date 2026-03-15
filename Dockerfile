FROM golang:1.24-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN mkdir -p /out \
    && go build -o /out/master ./cmd/master \
    && go build -o /out/slave ./cmd/slave \
    && go build -o /out/lnctl ./cmd/lnctl

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/master /usr/local/bin/master
COPY --from=builder /out/slave /usr/local/bin/slave
COPY --from=builder /out/lnctl /usr/local/bin/lnctl

EXPOSE 8080 50050 50051 53/tcp 53/udp 80 443

CMD ["master"]
