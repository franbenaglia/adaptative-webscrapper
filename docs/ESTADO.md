# Estado del proyecto

Última actualización: 2026-10-12

## Resumen

Fase 1 terminada y verificada: la CLI ejecuta una receta manual con Playwright sobre el sitio de
demostración local, recorre la paginación, aplica límites y exporta JSON y CSV. No hay todavía
persistencia, servidor HTTP, interfaz web ni IA.

## Fase actual

**Fase 2 — Persistencia y servidor**, sin empezar.

## Hecho

- Fase 0: especificación evaluada, documentación de continuidad, Go 1.27.2 instalado, git inicializado.
- Fase 1 (2026-10-12), verificada con `make check`, `make test-browser` y una ejecución real de la CLI
  contra `cmd/demosite` (6 registros en 2 páginas; código de salida 5 con receta inválida):
  - `internal/domain`: receta declarativa y su validación, solicitud, conversión de valores.
  - `internal/application`: `ScrapingService` y puertos `BrowserLauncher`, `BrowserSession`, `ProgressSink`.
  - `internal/browser`: adaptador Playwright (único paquete que importa `playwright-go`).
  - `internal/export`: JSON y CSV.
  - `internal/cli` y `cmd/scraper`: comandos `scrape` y `version`, códigos de salida.
  - `cmd/demosite`, `examples/demo`, `examples/recipes/productos-demo.json`, `Makefile`, `README.md`.

## En curso

Nada.

## Siguiente paso

Empezar la Fase 2 por este orden:

1. Decidir el driver SQLite (P-2 en `docs/DECISIONES.md`) y crear `migrations/` con `recipes`,
   `recipe_versions`, `jobs`, `job_events`, `records`.
2. Estados de trabajo con transiciones explícitas en `internal/domain` (`queued`, `running`, `completed`,
   `failed`, `blocked`, `cancelled`) y sus pruebas.
3. Repositorios SQLite detrás de puertos; añadir `RecipeID` y `JobID` a los tipos (D-006).
4. Cola en proceso con concurrencia configurable, persistencia incremental de registros y recuperación de panics.
5. API HTTP `/api/v1` con handlers delgados, SSE en `/jobs/{id}/events` y endpoint de salud.
6. Comandos `recipes` y `jobs` de la CLI.

## Pendientes conocidos

- Sin protección SSRF (Fase 5): no exponer el servicio a URL no confiables todavía (D-008).
- Sin detección de login, CAPTCHA o bloqueo: `domain.ErrBlocked` y su código de salida existen, pero nada lo emite aún.
- `number` es el único tipo numérico; faltan fechas y moneda (D-006).
- La comparación con ejecuciones anteriores requiere la persistencia de la Fase 2.

## Entorno de desarrollo (comprobado el 2026-10-12)

| Herramienta | Estado |
|---|---|
| Go | 1.27.2 en `~/sdk/go1.27.2`, enlazado desde `~/.local/bin/go` y `gofmt` (ya en el PATH). Instalado sin sudo; para actualizar, extraer otra versión en `~/sdk` y reapuntar los enlaces. |
| Node.js | v20.20.2 |
| Docker / Compose | Instalados (Compose v5.5.1) |
| curl | Es el de snap (`/snap/bin/curl`): no puede escribir en `/tmp` con `-o`; usar redirección `>` del shell. |
| git, make, gcc | Instalados |
| Ollama | **No instalado**. Solo hace falta desde la Fase 4. |
| Playwright | Driver 1.62.1 en `~/.cache/ms-playwright-go` (134 MB, con su propio Node.js) y Chromium headless en `~/.cache/ms-playwright`. Se reinstala con `make install-browsers`. |
| Hardware | 8 núcleos, 15 GB RAM, sin GPU NVIDIA: la inferencia local será solo por CPU. |
| Disco | 19 GB libres en `/home` (92 % usado). Vigilar: imágenes Docker, navegadores y modelos ocupan varios GB. |

## Fases (de la especificación)

- [x] Fase 1 — Base funcional: módulo Go, dominio, interfaces, CLI mínima, extracción determinista con receta manual, export JSON/CSV, pruebas.
- [ ] Fase 2 — Persistencia y servidor: SQLite, migraciones, API HTTP, cola, cancelación, SSE, historial.
- [ ] Fase 3 — Interfaz web: formulario, progreso, vista previa, historial, recetas, exportaciones.
- [ ] Fase 4 — IA adaptable: adaptador Ollama, snapshots, descubrimiento, validación, reparación, versiones.
- [ ] Fase 5 — Empaquetado y endurecimiento: Docker, SSRF completo, timeouts, trazas, documentación.

### Criterios de aceptación del primer hito (Fase 1)

- [x] Ejecutar una página HTML de ejemplo local.
- [x] Ejecutar la CLI con una receta manual.
- [x] Extraer nombre, precio y disponibilidad mediante Playwright.
- [x] Recorrer dos páginas mediante un botón siguiente.
- [x] Limitar páginas y registros.
- [x] Exportar los mismos resultados en JSON y CSV.
- [x] Ejecutar las pruebas sin conexión a Internet.
- [x] Probar que la CLI usa el mismo servicio de aplicación que usará la API.
