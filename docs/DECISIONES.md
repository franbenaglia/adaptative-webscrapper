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

## Propuestas pendientes de confirmar (aún no son decisiones)

Surgen de la evaluación de factibilidad del 2026-10-12. Conviértelas en entradas `D-NNN` al adoptarlas.

- **P-1 — Ruta del módulo de Playwright.** La especificación cita `github.com/mxschmitt/playwright-go`; el
  proyecto se trasladó a `github.com/playwright-community/playwright-go`. Verificar la ruta y la versión
  vigentes al iniciar la Fase 1 antes de fijar la dependencia.
- **P-2 — Driver SQLite sin CGO** (`modernc.org/sqlite`) para simplificar compilación cruzada y la imagen
  Docker. Alternativa: `mattn/go-sqlite3` (requiere CGO; gcc está disponible).
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
- **R-2 — `playwright-go` es un binding comunitario** que descarga su propio driver y exige versiones
  concretas de navegador. Mitigación: ya previsto por la especificación (adaptador detrás de interfaz,
  reemplazable por un worker Node.js).
- **R-3 — Detección de login, CAPTCHA y bloqueos es heurística**: habrá falsos positivos y negativos.
- **R-4 — Espacio en disco**: 19 GB libres; imagen con Chromium (~2 GB) más modelos de Ollama (2–5 GB cada uno).
- **R-5 — Alcance de la receta**: el modelo `ItemLocator` + campos cubre listados y tablas con botón
  "siguiente"; no cubre páginas de detalle, scroll infinito ni contenido en iframes. Aceptado para el MVP.
