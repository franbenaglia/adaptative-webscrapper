.PHONY: build test test-browser fmt vet check install-browsers demo-site demo server web-install web-build web-dev web-check

VERSION ?= dev

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/adaptive-scraper ./cmd/scraper
	go build -ldflags "-X main.version=$(VERSION)" -o bin/adaptive-scraper-server ./cmd/server
	go build -o bin/demosite ./cmd/demosite

# Pruebas rápidas: sin navegador, sin Internet.
test:
	go test -race ./...

# Pruebas de integración con Chromium real contra un servidor local.
test-browser:
	go test -tags browser ./internal/browser/...

fmt:
	gofmt -l -w .

vet:
	go vet ./...
	go vet -tags browser ./...

check: vet test
	@test -z "$$(gofmt -l .)" || (echo "archivos sin formatear:"; gofmt -l .; exit 1)

# Descarga el driver de Playwright y Chromium headless (una vez por versión).
install-browsers:
	go run github.com/mxschmitt/playwright-go/cmd/playwright install --only-shell chromium

demo-site:
	go run ./cmd/demosite

# API HTTP en APP_HTTP_ADDR (por defecto 127.0.0.1:8080).
server:
	go run ./cmd/server

# Requiere `make demo-site` en otra terminal.
demo: build
	bin/adaptive-scraper scrape --recipe examples/recipes/productos-demo.json --output bin/productos.csv

# Interfaz web (requiere Node.js 20.19 o posterior).
web-install:
	cd web && npm install

# Genera web/dist, que el servidor publica en la raíz.
web-build:
	cd web && npm run build

# Servidor de desarrollo de Vite con recarga; necesita `make server` en otra terminal.
web-dev:
	cd web && npm run dev

web-check:
	cd web && npm run check
