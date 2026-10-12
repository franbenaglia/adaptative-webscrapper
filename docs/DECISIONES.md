# Decisiones y riesgos

Registro de decisiones arquitectónicas y desviaciones respecto de `Claude_Code_AdaptiveScraper.md`.
Añade entradas al final; no reescribas las anteriores (si una cambia, añade otra que la sustituya).

Formato: `D-NNN — título`, fecha, estado (propuesta / aceptada / sustituida), contexto, decisión, consecuencias.

## Decisiones

### D-001 — Documentación de continuidad en el repositorio

- Fecha: 2026-10-12. Estado: aceptada (pedida por el usuario).
- Contexto: el desarrollo lo hace una IA en sesiones que pueden cortarse o perder contexto.
- Decisión: `CLAUDE.md` es el punto de entrada; `docs/ESTADO.md` refleja el estado actual; este archivo
  guarda las decisiones; cada unidad de trabajo verificada termina en un commit.
- Consecuencias: actualizar `docs/ESTADO.md` forma parte de la definición de "terminado" de cada tarea.

### D-002 — `playwright-go` se importa como `github.com/mxschmitt/playwright-go`

- Fecha: 2026-10-12. Estado: aceptada. Sustituye a la propuesta P-1, que era errónea.
- Contexto: se sospechaba que el módulo se había trasladado a `playwright-community`. Comprobado con
  `go get`: el `go.mod` de la versión vigente declara `github.com/mxschmitt/playwright-go`, como dice la especificación.
- Decisión: versión `v0.6201.1` (driver de Playwright 1.62.1). El driver se instala con `make install-browsers`.
- Consecuencias: no definir `PLAYWRIGHT_NODEJS_PATH` al instalar; con esa variable no se descarga el Node.js
  propio del driver y después falla en tiempo de ejecución si la variable no sigue definida.

### D-003 — La raíz del repositorio es esta carpeta y el módulo se llama `adaptive-scraper`

- Fecha: 2026-10-12. Estado: aceptada.
- Decisión: no se crea un subdirectorio `adaptive-scraper/`. Ruta de módulo sin dominio porque aún no hay
  repositorio remoto; cambiarla es un reemplazo mecánico de imports.

### D-004 — El puerto del navegador se divide en `BrowserLauncher` y `BrowserSession`

- Fecha: 2026-10-12. Estado: aceptada. Desviación de la interfaz `Browser` sugerida.
- Contexto: cada trabajo necesita su propio contexto aislado y habrá varios en paralelo (Fase 2).
- Decisión: `BrowserLauncher.Open` devuelve una `BrowserSession` (Navigate, URL, Extract, GoToNextPage, Close).
  `Snapshot` y `Screenshot` se añadirán en las Fases 4 y 5, cuando tengan un consumidor.
- Consecuencias: la cancelación cierra el navegador de la sesión, porque `playwright-go` no acepta `context.Context`.

### D-005 — Extracción con un único script fijo y solo selectores CSS

- Fecha: 2026-10-12. Estado: aceptada.
- Decisión: `internal/browser` evalúa un script constante que recibe la receta como datos y usa
  `querySelector`. La validación de recetas rechaza motores de Playwright (`xpath=`, `text=`...) y `>>`.
- Consecuencias: una sola ida y vuelta por página; la receta nunca aporta código. Si hiciera falta XPath o
  selección por texto habrá que ampliar el script y la validación a la vez.

### D-006 — Alcance reducido de los tipos en la Fase 1

- Fecha: 2026-10-12. Estado: aceptada.
- Decisión: `ScrapeRequest` lleva la receta en línea (`Recipe`); `RecipeID`, `Instruction` y `JobID` se añaden
  con la persistencia (Fase 2) y la IA (Fase 4). Tipos de campo: `text`, `number`, `url`; fechas y moneda
  explícita quedan para más adelante. `number` acepta formatos "1.234,50" y "1,234.50".
- Consecuencias: un separador único seguido de tres dígitos se interpreta como miles ("1.234" es 1234).

### D-007 — Los comandos de Cobra viven en `internal/cli`

- Fecha: 2026-10-12. Estado: aceptada.
- Decisión: `cmd/scraper/main.go` solo cablea dependencias. `internal/cli` recibe un
  `application.ScrapingService`, lo que permite probar con un servicio simulado que la CLI no contiene
  lógica de scraping (criterio 8 del primer hito).

### D-008 — Pospuestos respecto del texto de la Fase 1

- Fecha: 2026-10-12. Estado: aceptada.
- Decisión: el esqueleto del frontend se crea en la Fase 3 (vacío no aporta nada y ocupa disco). La
  protección SSRF llega en la Fase 5: **hoy no se bloquean redes privadas**; la única barrera es que la
  navegación no puede salir de los hosts permitidos por la receta. La configuración se limita a `PLAYWRIGHT_HEADLESS`.

### D-009 — SQLite con `modernc.org/sqlite` (sin CGO)

- Fecha: 2026-10-12. Estado: aceptada (confirmada por el usuario). Sustituye a la propuesta P-2.
- Decisión: driver en Go puro, una sola conexión abierta por proceso y modo WAL.
- Consecuencias: binarios estáticos y Dockerfile sin toolchain de C. Las migraciones viven en `migrations/`
  como paquete Go con `embed`, para que el binario las lleve dentro.

## Propuestas pendientes de confirmar (aún no son decisiones)

Surgen de la evaluación de factibilidad del 2026-10-12. Conviértelas en entradas `D-NNN` al adoptarlas.

- **P-3 — El modelo elige entre candidatos, no inventa selectores.** El snapshot numera los candidatos de
  contenedores repetidos y de campos, calculados de forma determinista; el modelo devuelve identificadores
  de candidato y nombres de campo. Reduce mucho las alucinaciones de un modelo pequeño y simplifica la validación.
- **P-4 — Salida estructurada de Ollama** (parámetro `format` con JSON Schema) además de la decodificación
  estricta y la validación semántica propias.
- **P-5 — SSRF aplicado en el navegador**, interceptando todas las peticiones (`Route`) y validando host e IP
  de cada una, no solo con una resolución DNS previa (que es vulnerable a DNS rebinding y no cubre
  subrecursos ni redirecciones).

## Riesgos abiertos

- **R-1 — Calidad y velocidad del modelo local (riesgo principal).** Sin GPU y con 15 GB de RAM solo caben
  modelos pequeños en CPU: lentos y poco fiables generando selectores. Mitigaciones: P-3, P-4, reparación
  acotada, y mantener `IntelligenceProvider` intercambiable por un proveedor remoto. Las Fases 1–3 no dependen de esto.
- **R-2 — `playwright-go` es un binding no oficial** que descarga su propio driver y exige versiones
  concretas de navegador. Mitigación: ya previsto por la especificación (adaptador detrás de interfaz,
  reemplazable por un worker Node.js).
- **R-3 — Detección de login, CAPTCHA y bloqueos es heurística**: habrá falsos positivos y negativos.
- **R-4 — Espacio en disco**: 19 GB libres; imagen con Chromium (~2 GB) más modelos de Ollama (2–5 GB cada uno).
- **R-5 — Alcance de la receta**: el modelo `ItemLocator` + campos cubre listados y tablas con botón
  "siguiente"; no cubre páginas de detalle, scroll infinito ni contenido en iframes. Aceptado para el MVP.
