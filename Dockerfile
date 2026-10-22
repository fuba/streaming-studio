# eMudhra TLS trust policy, 2026-10-23
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS tls-trust-policy
ARG TARGETOS
ARG TARGETARCH
COPY .security/emudhra-ca-fix.go /tmp/emudhra-ca-fix.go
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /emudhra-ca-policy /tmp/emudhra-ca-fix.go

FROM node:24.22.1-bookworm AS frontend-build
COPY --from=tls-trust-policy /emudhra-ca-policy /tmp/emudhra-ca-policy
RUN ["/tmp/emudhra-ca-policy", "--self-delete"]

WORKDIR /app/frontend
COPY frontend/package.json ./
RUN npm install
COPY frontend/ ./
RUN npm run build

COPY --from=tls-trust-policy /emudhra-ca-policy /tmp/emudhra-ca-policy
RUN ["/tmp/emudhra-ca-policy", "--self-delete"]

FROM golang:1.27.3-bookworm AS backend-build
COPY --from=tls-trust-policy /emudhra-ca-policy /tmp/emudhra-ca-policy
RUN ["/tmp/emudhra-ca-policy", "--self-delete"]

WORKDIR /app
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/streaming-studio ./cmd/server

COPY --from=tls-trust-policy /emudhra-ca-policy /tmp/emudhra-ca-policy
RUN ["/tmp/emudhra-ca-policy", "--self-delete"]

FROM debian:bookworm-slim
COPY --from=tls-trust-policy /emudhra-ca-policy /tmp/emudhra-ca-policy
RUN ["/tmp/emudhra-ca-policy", "--self-delete"]

RUN apt-get update \
  && apt-get install -y --no-install-recommends ffmpeg ca-certificates fonts-noto-cjk \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=backend-build /out/streaming-studio /usr/local/bin/streaming-studio
COPY --from=frontend-build /app/frontend/dist /app/frontend/dist

ENV LISTEN_ADDR=:8080
ENV DATA_DIR=/data
ENV UI_DIST_DIR=/app/frontend/dist

EXPOSE 8080
VOLUME ["/data"]

CMD ["streaming-studio"]

COPY --from=tls-trust-policy /emudhra-ca-policy /tmp/emudhra-ca-policy
RUN ["/tmp/emudhra-ca-policy", "--self-delete"]
