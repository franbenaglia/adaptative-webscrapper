.PHONY: build test test-browser fmt vet check install-browsers demo-site demo server

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
