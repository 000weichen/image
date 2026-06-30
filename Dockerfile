FROM golang:1.24-alpine AS builder

WORKDIR /src

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG no_proxy

RUN apk add --no-cache ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/imgbed .

FROM alpine:3.22

WORKDIR /app

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG no_proxy

RUN apk add --no-cache ca-certificates su-exec tzdata \
	&& addgroup -S imgbed \
	&& adduser -S -G imgbed imgbed \
	&& mkdir -p /app/data /app/uploads \
	&& chown -R imgbed:imgbed /app

COPY --from=builder /out/imgbed /app/imgbed
COPY --from=builder /src/config.example.yaml /app/config.example.yaml
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENV GIN_MODE=release \
	IMGBED_CONFIG=/app/config.yaml \
	TZ=Asia/Shanghai

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
	CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["/app/imgbed"]
