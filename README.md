# AdaptiveScraper

Extracción web local basada en recetas declarativas. Una receta describe qué elementos se repiten en una
página, qué campos tiene cada uno y cómo pasar a la página siguiente; el motor la ejecuta con Playwright de
forma determinista. En fases posteriores un modelo local (Ollama) descubrirá y reparará las recetas.

**Estado: Fase 3 de 5.** Hay interfaz web, CLI y API HTTP con recetas escritas a mano e historial en
SQLite. Aún no hay IA.
El avance detallado está en [docs/ESTADO.md](docs/ESTADO.md) y las decisiones en
[docs/DECISIONES.md](docs/DECISIONES.md).

## Requisitos

- Go 1.27 o posterior y `make`.
- Node.js 20.19 o posterior, solo para compilar la interfaz web.
- Conexión a Internet solo para la instalación inicial (dependencias, driver de Playwright y Chromium).

## Instalación

```bash
make install-browsers   # una vez: driver de Playwright y Chromium headless
make build              # genera bin/adaptive-scraper, bin/adaptive-scraper-server y bin/demosite
make web-install        # una vez: dependencias de la interfaz web
make web-build          # genera web/dist
```

## Interfaz web

```bash
make server             # abre http://127.0.0.1:8080
```

- **Nueva extracción**: elige una receta guardada o pega una en JSON, ajusta URL, límites y modo headless.
- **Trabajo**: progreso en vivo, avisos, vista previa de registros, exportación a CSV y JSON, cancelar,
  volver a ejecutar y guardar la receta usada.
- **Trabajos** y **Recetas**: historial y gestión de recetas (crear, editar, ejecutar, borrar).

Para desarrollar la interfaz con recarga automática: `make server` en una terminal y `make web-dev` en otra.

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

Cada ejecución queda registrada. Otros comandos:

```text
adaptive-scraper recipes import <archivo.json>   guarda una receta y muestra su id
adaptive-scraper recipes list | show <id>
adaptive-scraper recipes run <id>                acepta los mismos flags que scrape
adaptive-scraper scrape --recipe-id <id>
adaptive-scraper jobs list | show <id>
adaptive-scraper version
```

`list` admite `--json`.

## Servidor y API HTTP

```bash
make server             # o bin/adaptive-scraper-server; escucha en 127.0.0.1:8080
```

El servidor y la CLI comparten la base de datos (`data/adaptive-scraper.db`), así que ven las mismas
recetas y el mismo historial si se ejecutan desde el mismo directorio.

```bash
API=http://127.0.0.1:8080/api/v1
curl -X POST -H 'Content-Type: application/json' --data @examples/recipes/productos-demo.json $API/recipes
curl -X POST -H 'Content-Type: application/json' -d '{"max_pages": 5}' $API/recipes/<id>/run
curl -N $API/jobs/<job>/events                   # progreso en vivo (Server-Sent Events)
curl "$API/jobs/<job>/export?format=csv"
```

| Método y ruta | Descripción |
|---|---|
| `POST /api/v1/jobs` | Crea y encola un trabajo. Cuerpo: `recipe_id` o `recipe`, y opcionalmente `url`, `max_pages`, `max_records`, `headless`. Responde `202`. |
| `GET /api/v1/jobs` | Historial; admite `status` y `limit`. |
| `GET /api/v1/jobs/{id}` | Estado, contadores, avisos y error. |
| `POST /api/v1/jobs/{id}/cancel` | Cancela un trabajo en cola o en curso. |
| `GET /api/v1/jobs/{id}/records` | Registros paginados (`offset`, `limit`). |
| `GET /api/v1/jobs/{id}/events` | SSE: `job.started`, `page.opened`, `records.extracted`, `job.completed`, `job.failed`, `job.blocked`, `job.cancelled`. Reanuda con `Last-Event-ID`. |
| `GET /api/v1/jobs/{id}/export?format=csv\|json` | Descarga de resultados. |
| `GET`, `POST /api/v1/recipes` | Lista o crea recetas. |
| `GET`, `PUT`, `DELETE /api/v1/recipes/{id}` | Lee, actualiza (nueva versión) o borra. |
| `POST /api/v1/recipes/{id}/run` | Ejecuta una receta guardada. |
| `GET /healthz` | Comprobación de salud. |

Los `POST` requieren `Content-Type: application/json`. Los errores tienen la forma `{"error": "..."}`.

Estados de un trabajo: `queued` → `running` → `completed`, `failed`, `blocked` o `cancelled`.

## Configuración

Variables de entorno o archivo `.env` (ver [.env.example](.env.example)):

| Variable | Por defecto | Descripción |
|---|---|---|
| `APP_HTTP_ADDR` | `127.0.0.1:8080` | Dirección del servidor. |
| `APP_DATA_DIR` | `data` | Directorio de datos. |
| `APP_DATABASE_PATH` | `<APP_DATA_DIR>/adaptive-scraper.db` | Base SQLite. |
| `APP_MAX_CONCURRENT_JOBS` | `2` | Navegadores simultáneos en el servidor. |
| `APP_JOB_TIMEOUT` | `10m` | Duración máxima de un trabajo. |
| `PLAYWRIGHT_HEADLESS` | `true` | Navegador sin ventana. |
| `APP_WEB_DIR` | `web/dist` | Interfaz web compilada. |

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
make check          # vet, pruebas con -race y formato; no necesita navegador ni Internet
make test-browser   # pruebas de integración con Chromium contra un servidor local
make web-check      # tipos y compilación de la interfaz web
```

## Uso responsable

AdaptiveScraper no resuelve CAPTCHA, no evade sistemas anti-bot y no elude controles de acceso. Quien lo
usa es responsable de respetar las condiciones de uso de cada sitio, `robots.txt`, los derechos sobre los
datos y los límites de acceso.

La protección contra SSRF todavía no está implementada y la API no tiene autenticación: no ejecutes recetas
o URL de origen no confiable ni expongas el servidor fuera de `127.0.0.1`.
