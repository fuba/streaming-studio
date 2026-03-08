FROM node:23-bookworm AS frontend-build

WORKDIR /app/frontend
COPY frontend/package.json ./
RUN npm install
COPY frontend/ ./
RUN npm run build

FROM golang:1.22-bookworm AS backend-build

WORKDIR /app
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/streaming-studio ./cmd/server

FROM debian:bookworm-slim

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
