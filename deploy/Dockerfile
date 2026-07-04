ARG BIFROST_REF=v1.6.2
ARG GO_VERSION=1.26.4

FROM alpine/git AS source
ARG BIFROST_REF
WORKDIR /src
RUN git clone --depth 1 --branch "${BIFROST_REF}" https://github.com/maximhq/bifrost .
COPY plugins/pxpipe ./plugins/pxpipe

FROM node:25-alpine AS ui-builder
WORKDIR /app
RUN apk upgrade --no-cache
COPY --from=source /src/ui ./
RUN npm ci && npm run build-enterprise

FROM golang:${GO_VERSION}-alpine AS builder
RUN apk upgrade --no-cache && apk add --no-cache gcc musl-dev sqlite-dev binutils binutils-gold
WORKDIR /app
COPY --from=source /src ./
COPY --from=ui-builder /app/out ./transports/bifrost-http/ui
RUN sh ./plugins/pxpipe/deploy/register.sh .
WORKDIR /app/transports
ENV CGO_ENABLED=1 GOOS=linux GOWORK=off
ARG VERSION=pxpipe
RUN go build -ldflags="-w -s -X main.Version=v${VERSION} -extldflags '-static'" \
    -a -trimpath -tags "sqlite_static" -o /out/bifrost-http ./bifrost-http

FROM alpine:3.23
RUN apk upgrade --no-cache && apk add --no-cache musl libgcc ca-certificates zlib
COPY --from=builder /out/bifrost-http /usr/local/bin/bifrost-http
ENV APP_DIR=/app/data
RUN mkdir -p "$APP_DIR/logs"
VOLUME ["/app/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1
ENTRYPOINT ["bifrost-http", "-app-dir", "/app/data", "-host", "0.0.0.0", "-port", "8080"]
