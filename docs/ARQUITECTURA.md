# Arquitectura

Resumen de cómo está construido AdaptiveScraper. El porqué de cada decisión está en
[DECISIONES.md](DECISIONES.md); aquí solo se describe el resultado.

## Capas

Arquitectura de puertos y adaptadores. Las dependencias apuntan siempre hacia dentro: el dominio no
conoce a nadie, la aplicación solo conoce el dominio y los adaptadores conocen a ambos.

```text
              cmd/scraper (CLI)            cmd/server (HTTP)
                    │                            │
                    └──────── internal/bootstrap ┘      raíz de composición: une adaptadores y servicios
                                     │
     adaptadores de entrada          │          adaptadores de salida
   ┌──────────────┬──────────────┐   │   ┌──────────────┬──────────────┬───────────────┬──────────────┐
   │ internal/cli │ internal/api │   │   │ browser      │ intelligence │ storage/sqlite│ export       │
   │ (Cobra)      │ (net/http,   │   │   │ (Playwright, │ (prompt,     │ (repositorios,│ (JSON, CSV)  │
   │              │  SSE, web)   │   │   │  netguard)   │  Ollama)     │  migraciones) │              │
   └──────┬───────┴──────┬───────┘   │   └──────▲───────┴──────▲───────┴───────▲───────┴──────────────┘
          │              │           │          │ implementan los puertos       │
          ▼              ▼           ▼          │              │               │
   ┌───────────────────────────────────────────┴──────────────┴───────────────┴──────┐
   │ internal/application                                                             │
   │   JobService  ── caso de uso compartido por CLI y API                            │
   │   Service     ── motor de extracción (ScrapingService)                           │
   │   RecipeService, Queue, EventHub                                                 │
   │   puertos: BrowserLauncher, BrowserSession, IntelligenceProvider,                │
   │            JobRepository, RecipeRepository, JobQueue, ProgressSink               │
   └───────────────────────────────────┬──────────────────────────────────────────────┘
                                       ▼
   ┌──────────────────────────────────────────────────────────────────────────────────┐
   │ internal/domain   Recipe, Job y sus estados, JobEvent, PageSnapshot, validación  │
   └──────────────────────────────────────────────────────────────────────────────────┘
```

| Paquete | Responsabilidad | Importa de terceros |
|---|---|---|
| `internal/domain` | Tipos y reglas: validar recetas, transiciones de estado, conversión de valores. | Nada |
| `internal/application` | Casos de uso y puertos. | Nada |
| `internal/browser` | Chromium con Playwright: navegar, extraer, paginar, resumir, diagnosticar. | `playwright-go` |
| `internal/netguard` | Política de red y proxy de filtrado. | Nada |
| `internal/intelligence` | Traducir entre resumen de página, modelo y receta; cliente de Ollama. | Nada (HTTP estándar) |
| `internal/storage/sqlite` | Repositorios y migraciones. | `modernc.org/sqlite` |
| `internal/export` | CSV y JSON. | Nada |
| `internal/api` | Rutas HTTP, SSE, archivos de la interfaz web. | Nada |
| `internal/cli` | Comandos. | `cobra` |
| `internal/config` | Variables de entorno y `.env`. | Nada |
| `internal/bootstrap` | Construir todo lo anterior. | — |
| `web/` | Interfaz en React + TypeScript; habla solo con la API. | React, Vite |

## Recorrido de un trabajo

1. La CLI o la API construyen un `JobRequest` y llaman a `JobService.Create`, único lugar donde se valida.
   El trabajo se guarda en estado `queued`.
2. En el servidor, la `Queue` lo entrega a un worker cuando hay hueco; en la CLI se ejecuta al momento. En
   ambos casos es `JobService.Execute`.
3. `Execute` pasa el trabajo a `running` y llama al motor (`Service.Run`) con un `ProgressSink`.
4. El motor abre una sesión de navegador propia, navega y:
   - con receta, la ejecuta;
   - sin receta, pide un `PageSnapshot` al navegador, se lo pasa al `IntelligenceProvider`, valida la
     receta propuesta y la prueba;
   - si la receta no funciona en la primera página, intenta repararla un número limitado de veces.
5. Por cada página el motor emite eventos. `JobService` guarda en ese momento los registros nuevos y el
   evento, y los publica en el `EventHub`, del que leen las conexiones SSE.
6. Al terminar, el trabajo pasa a `completed`, `failed`, `blocked` o `cancelled`, y el navegador se cierra
   siempre. Si falló y los diagnósticos están activos, antes se guardan una captura y la traza.

## Dónde interviene el modelo

El modelo nunca recibe HTML ni escribe selectores. El navegador ejecuta un script fijo que resume la página
en candidatos numerados; el modelo devuelve identificadores de esa lista, restringidos por un esquema JSON;
`internal/intelligence` los traduce a una receta y el motor la valida y la prueba. Una receta que funciona
se ejecuta sin consultar al modelo.

## Seguridad

| Riesgo | Medida | Dónde |
|---|---|---|
| Usar el navegador para leer servicios internos (SSRF) | Todo el tráfico de Chromium pasa por un proxy local que bloquea direcciones locales, privadas y reservadas, y conecta a la IP ya comprobada. Cubre redirecciones, subrecursos y DNS rebinding. | `internal/netguard`, `internal/browser` |
| Recetas o respuestas del modelo maliciosas | Recetas declarativas, validación estricta, selectores solo CSS, script de extracción fijo. Nada generado se ejecuta. | `internal/domain`, `internal/intelligence` |
| Navegar fuera del sitio previsto | Cada página debe pertenecer a los hosts de la receta. | `internal/application` |
| Otra web atacando la API local | Solo escucha en loopback; valida `Host`; los `POST` exigen JSON; sin CORS. | `internal/api` |
| Descargas y respuestas enormes | Descargas desactivadas; límite de bytes por conexión; tiempos máximos de navegación, acción, consulta al modelo y trabajo. | `internal/browser`, `internal/netguard`, `internal/config` |
| Evadir protecciones de los sitios | No se hace: login, CAPTCHA y 401/403/429 terminan el trabajo como `blocked`. | `internal/browser`, `internal/application` |

## Datos

SQLite en `<APP_DATA_DIR>/adaptive-scraper.db`, con migraciones en `migrations/`:

| Tabla | Contenido |
|---|---|
| `recipes` | Recetas guardadas, versión vigente. |
| `recipe_versions` | Todas las versiones de cada receta. |
| `jobs` | Trabajos, con copia de la receta usada, estado, contadores, avisos y error. |
| `job_events` | Eventos de cada trabajo, numerados. |
| `records` | Registros extraídos, en orden. |

Los diagnósticos van a `<APP_DATA_DIR>/diagnostics/<id del trabajo>/`.

## Pruebas

| Comando | Qué ejercita | Necesita |
|---|---|---|
| `make check` | Dominio, casos de uso con navegador e IA simulados, SQLite, API, CLI, proxy. | Nada externo |
| `make test-browser` | Chromium real contra servidores locales: extracción, resumen, política de red, diagnósticos. | Navegador instalado |
| `make test-ai` | Descubrir y reparar con un modelo real. | Ollama y el modelo |
| `make web-check` | Tipos y compilación de la interfaz. | Node.js |
