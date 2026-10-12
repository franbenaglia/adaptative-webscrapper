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

### D-010 — `JobService` es el caso de uso compartido; `ScrapingService` queda como motor

- Fecha: 2026-10-12. Estado: aceptada. Amplía D-007.
- Contexto: con persistencia, tanto la CLI como la API necesitan crear un trabajo, ejecutarlo y dejar
  historial. La validación debe existir en un solo sitio.
- Decisión: `application.JobService` (`Create`, `Execute`, `Cancel`, `Reject`, `RecoverInterrupted`, lecturas)
  es lo que invocan `internal/cli` e `internal/api`. `ScrapingService.Run` recibe ahora un `ProgressSink`
  por ejecución y solo lo llama `JobService`. `internal/bootstrap` es la única raíz de composición y la usan
  `cmd/server` y `cmd/scraper`.
- Consecuencias: la CLI ejecuta el trabajo en su propio proceso pero escribe en la misma base SQLite que el
  servidor, así que su historial aparece en la API. Un trabajo en curso solo puede cancelarse desde el
  proceso que lo ejecuta. El futuro modo `--server` de la CLI será otro cliente de la misma API.

### D-011 — Cada trabajo guarda una copia de su receta

- Fecha: 2026-10-12. Estado: aceptada.
- Decisión: `jobs.recipe` contiene la receta usada (JSON) y `jobs.recipe_id` no es clave foránea.
- Consecuencias: se pueden ejecutar recetas en línea sin guardarlas, y borrar o editar una receta no altera
  el historial ni la exportación de trabajos anteriores.

### D-012 — Guardado incremental y eventos

- Fecha: 2026-10-12. Estado: aceptada.
- Decisión: el motor emite los registros nuevos de cada página en el evento `records.extracted`;
  `JobService` los guarda en ese momento, actualiza los contadores del trabajo y registra un `job_event`
  con `seq` creciente. El estado final se guarda con un contexto no cancelable.
- Consecuencias: un fallo o cancelación tardíos conservan lo extraído. SSE usa `seq` como `id`, de modo que
  `Last-Event-ID` reanuda sin duplicados; primero se suscribe al `EventHub` y después lee el historial.
  Un suscriptor lento se descarta y debe reconectar.

### D-013 — Protección mínima de la API local

- Fecha: 2026-10-12. Estado: aceptada.
- Contexto: la API no tiene autenticación y se pensó para uso local.
- Decisión: escucha por defecto en `127.0.0.1:8080`; todo `POST` exige `Content-Type: application/json`
  (impide CSRF desde formularios de otras webs); no se envían cabeceras CORS; cuerpos limitados a 1 MB y
  decodificación estricta.
- Consecuencias: el frontend de la Fase 3 debe servirse desde el mismo origen (o con proxy de Vite en
  desarrollo). Falta validar la cabecera `Host` contra DNS rebinding: pendiente para la Fase 5.

### D-014 — Añadidos no pedidos en la especificación

- Fecha: 2026-10-12. Estado: aceptada.
- Decisión: comando `recipes import <archivo>` (sin él la CLI no podía guardar recetas), flag `--recipe-id`
  en `scrape`, variable `APP_JOB_TIMEOUT` (duración máxima de un trabajo, 10 min por defecto) y evento
  `job.cancelled`.

### D-015 — Interfaz web sin librerías de UI ni router, servida desde disco

- Fecha: 2026-10-12. Estado: aceptada.
- Decisión: `web/` usa solo React, TypeScript y Vite. Rutas por hash (`#/jobs/<id>`), CSS propio con tema
  claro y oscuro, cliente tipado en `web/src/api.ts`. `cmd/server` publica `APP_WEB_DIR` (por defecto
  `web/dist`) en la raíz si contiene `index.html`; si no, arranca solo con la API y lo avisa en el log.
- Consecuencias: el binario no incrusta la interfaz, así que `go build` no depende de Node.js; la imagen
  Docker de la Fase 5 deberá copiar `web/dist`. En desarrollo, Vite hace de proxy de `/api` (D-013).
  La receta se edita como JSON: el cliente solo comprueba la sintaxis y el servidor valida las reglas.
- Sin pruebas automáticas de frontend por ahora: `npm run check` (tipos + compilación) y verificación manual
  en navegador. Si la interfaz crece, añadir Vitest.

### D-016 — El modelo elige entre candidatos numerados; nunca escribe selectores

- Fecha: 2026-10-12. Estado: aceptada. Adopta las propuestas P-3 y P-4.
- Contexto: el hardware solo permite modelos pequeños en CPU, poco fiables generando CSS.
- Decisión: `internal/browser/snapshot.go` resume la página con un script fijo en grupos de elementos
  repetidos (`g1`), campos dentro de cada elemento (`g1.f2`, con muestras) y controles de página siguiente
  (`n1`). El prompt lista identificadores y muestras, sin selectores ni HTML. El modelo devuelve
  `{group, fields:[{name, candidate, type, required}], next}` y `internal/intelligence` lo traduce a una
  receta. El esquema JSON que se pasa a Ollama (`format`) enumera los identificadores válidos, así que la
  generación no puede salirse de ellos; aun así se decodifica de forma estricta y se comprueba cada id.
- Consecuencias: los selectores son siempre CSS válido generado por nosotros. El tipo propuesto se corrige
  con las muestras (un `href` es `url`; solo es `number` lo que se puede leer como número). Si el dato que
  quiere el usuario no está entre los candidatos (por ejemplo dentro de un iframe o en un atributo que el
  script no mira), el modelo no puede encontrarlo: hay que ampliar el script.
- `intelligence.LLM` (una función `Complete` con esquema) es el único punto que conoce a Ollama; otro
  proveedor, local o remoto, solo implementa esa interfaz.

### D-017 — Cuándo interviene el modelo

- Fecha: 2026-10-12. Estado: aceptada.
- Decisión: con receta, la ejecución es determinista y el modelo no se consulta. Solo se consulta (a) sin
  receta, para descubrirla, y (b) si en la **primera página** la receta no encuentra elementos o un campo
  requerido falta en más de la mitad (`domain.AssessExtraction`), para repararla, con un máximo de
  `AI_MAX_REPAIRS` intentos (2 por defecto).
- La reparación conserva identidad, nombres, tipos y obligatoriedad de los campos, y solo se acepta si la
  receta nueva extrae bien en esa misma página. Si el trabajo termina `completed` y partía de una receta
  guardada, la reparada se guarda sola como versión siguiente. Una receta descubierta no se guarda sola:
  queda en el trabajo y el usuario decide (botón en la web, `--save-recipe` en la CLI).
- Consecuencias: una rotura que aparezca en páginas posteriores no se repara (solo genera avisos). Una
  receta recibida que no se puede reparar termina `completed` sin registros y con avisos; una descubierta
  que no extrae nada termina `failed`.

### D-018 — Detección de bloqueos

- Fecha: 2026-10-12. Estado: aceptada.
- Decisión: `Navigate` convierte 401, 403 y 429 en `domain.ErrBlocked`. Además, antes de descubrir y
  cuando una receta no encuentra nada, un script fijo busca CAPTCHA, textos de verificación humana o un
  campo de contraseña, y solo los considera bloqueo si la página tiene poco texto (menos de 1500
  caracteres). Ante un bloqueo el trabajo termina `blocked` sin consultar al modelo.
- Consecuencias: es una heurística (riesgo R-3): una página grande con un CAPTCHA incrustado no se detecta
  y una página pequeña legítima con un formulario de acceso sí.

### D-019 — Configuración de la IA y elección de modelo

- Fecha: 2026-10-12. Estado: aceptada (el usuario pidió Ollama sin Docker, modelo pequeño y configurable).
- Decisión: `AI_ENABLED` (false por defecto), `OLLAMA_BASE_URL`, `OLLAMA_MODEL`, `AI_MAX_REPAIRS`,
  `AI_TIMEOUT`. Cualquier usuario elige el modelo poniendo `OLLAMA_MODEL` en `.env` y descargándolo con
  `make ollama-pull`. `GET /api/v1/ai`, `adaptive-scraper ai status` y el formulario web muestran el modelo
  activo y si está instalado. `make test-ai` ejecuta el recorrido real (descubrir tarjetas, descubrir una
  tabla, reparar tras un rediseño) con el modelo configurado: es la forma de evaluar un modelo antes de adoptarlo.
- Consecuencias: el modelo es global al proceso, no se elige por trabajo.

## Propuestas pendientes de confirmar (aún no son decisiones)

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
