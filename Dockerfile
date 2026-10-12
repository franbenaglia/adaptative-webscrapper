# syntax=docker/dockerfile:1

# --- Interfaz web ---
FROM node:20-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- Binarios Go (estáticos: el driver SQLite no usa CGO) ---
FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/adaptive-scraper ./cmd/scraper \
 && go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/adaptive-scraper-server ./cmd/server \
 && go build -trimpath -o /out/playwright github.com/mxschmitt/playwright-go/cmd/playwright

# --- Imagen final ---
FROM debian:bookworm-slim
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright \
    PLAYWRIGHT_DRIVER_PATH=/opt/playwright-driver

# El instalador de playwright-go descarga el driver y Chromium headless e
# instala con apt las bibliotecas que necesita; después ya no hace falta.
COPY --from=build /out/playwright /usr/local/bin/playwright
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && playwright install --with-deps --only-shell chromium \
 && rm -rf /var/lib/apt/lists/* /usr/local/bin/playwright \
 && useradd --create-home --uid 10001 scraper \
 && mkdir /data && chown scraper:scraper /data

COPY --from=build /out/adaptive-scraper /out/adaptive-scraper-server /usr/local/bin/
COPY --from=web /src/web/dist /app/web

# Dentro del contenedor hay que escuchar en todas las interfaces; quien
# decide desde dónde se accede es la publicación del puerto (ver compose.yaml).
ENV APP_HTTP_ADDR=0.0.0.0:8080 \
    APP_DATA_DIR=/data \
    APP_WEB_DIR=/app/web \
    PLAYWRIGHT_HEADLESS=true

USER scraper
WORKDIR /data
VOLUME /data
EXPOSE 8080
CMD ["adaptive-scraper-server"]
