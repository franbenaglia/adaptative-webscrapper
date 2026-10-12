# AdaptiveScraper

Extracción web local basada en recetas declarativas. Una receta describe qué elementos se repiten en una
página, qué campos tiene cada uno y cómo pasar a la página siguiente; el motor la ejecuta con Playwright de
forma determinista. En fases posteriores un modelo local (Ollama) descubrirá y reparará las recetas.

**Estado: Fase 1 de 5.** Hay CLI con recetas escritas a mano. Aún no hay servidor, interfaz web ni IA.
El avance detallado está en [docs/ESTADO.md](docs/ESTADO.md) y las decisiones en
[docs/DECISIONES.md](docs/DECISIONES.md).

## Requisitos

- Go 1.27 o posterior y `make`.
- Conexión a Internet solo para la instalación inicial (dependencias, driver de Playwright y Chromium).

## Instalación

```bash
make install-browsers   # una vez: driver de Playwright y Chromium headless
make build              # genera bin/adaptive-scraper y bin/demosite
```

## Prueba rápida con el sitio de demostración

```bash
make demo-site          # terminal 1: sirve examples/demo en http://127.0.0.1:8090
make demo               # terminal 2: escribe bin/productos.csv
```

## Uso

```bash
bin/adaptive-scraper scrape \
  --recipe examples/recipes/productos-demo.json \
  --max-pages 5 \
  --max-records 200 \
  --output productos.csv
```

| Flag | Descripción |
|---|---|
| `--recipe` | Archivo JSON con la receta (obligatorio por ahora). |
| `--url` | URL inicial; por defecto, `start_url` de la receta. |
| `--max-pages` | Máximo de páginas (por defecto 5). |
| `--max-records` | Máximo de registros (por defecto 100). |
| `--output`, `-o` | Archivo de salida; sin él, los datos van a stdout. |
| `--format` | `json` o `csv`; por defecto según la extensión de `--output`, o `json`. |
| `--headless` | Navegador sin ventana (por defecto `true`, o `PLAYWRIGHT_HEADLESS`). |

El progreso y los avisos se escriben en stderr; los datos, en stdout o en el archivo.

Códigos de salida: `0` correcto, `1` error general, `3` bloqueo, `4` cancelación, `5` receta inválida.

## Formato de una receta

```json
{
  "name": "Productos de demostración",
  "start_url": "http://127.0.0.1:8090/productos.html",
  "item_locator": "article.product",
  "fields": [
    {"name": "nombre", "type": "text", "locator": ".name", "required": true},
    {"name": "precio", "type": "number", "locator": ".price", "required": true},
    {"name": "enlace", "type": "url", "locator": ".name a", "attribute": "href"}
  ],
  "pagination": {"type": "next_button", "next_locator": "button.next"}
}
```

- `item_locator` y `locator` son selectores CSS; `locator` es relativo a cada elemento y puede omitirse
  para usar el propio elemento.
- `type`: `text`, `number` (acepta "1.234,50" y "1,234.50") o `url` (se resuelve a absoluta).
- `attribute`: atributo a leer; sin él se usa el texto visible.
- `allowed_hosts`: hosts a los que puede llegar la navegación; por defecto, solo el de `start_url`.
- `pagination.type`: `none` o `next_button`.

## Desarrollo

```bash
make check          # vet, pruebas rápidas y formato; no necesita navegador ni Internet
make test-browser   # pruebas de integración con Chromium contra un servidor local
```

## Uso responsable

AdaptiveScraper no resuelve CAPTCHA, no evade sistemas anti-bot y no elude controles de acceso. Quien lo
usa es responsable de respetar las condiciones de uso de cada sitio, `robots.txt`, los derechos sobre los
datos y los límites de acceso.

La protección contra SSRF todavía no está implementada: no lo ejecutes con recetas o URL de origen no confiable.
