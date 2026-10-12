# Estado del proyecto

Última actualización: 2026-10-12

## Resumen

Fases 1 y 2 terminadas y verificadas. Hay motor determinista con Playwright, CLI, persistencia SQLite,
cola de trabajos y API HTTP con SSE. No hay todavía interfaz web ni IA.

## Fase actual

**Fase 3 — Interfaz web**, sin empezar.

## Hecho

- Fase 0: especificación evaluada, documentación de continuidad, Go 1.27.2 instalado, git inicializado.
- Fase 1 (2026-10-12): dominio, motor, adaptador Playwright, exportadores, CLI, sitio de demostración.
- Fase 2 (2026-10-12), verificada con `make check` (incluye `-race`), `make test-browser` y una ejecución
  real: servidor + sitio de demostración, receta creada por API, trabajo lanzado con
  `POST /recipes/{id}/run`, seis eventos por SSE, exportación CSV y el mismo historial visto desde la CLI.
  - `internal/domain/job.go`: estados, transiciones, `Job`, `JobEvent`.
  - `migrations/` e `internal/storage/sqlite`: esquema y repositorios.
  - `internal/application`: `JobService`, `RecipeService`, `EventHub`, `Queue`.
  - `internal/api`: API `/api/v1` con SSE; `cmd/server`.
  - `internal/config`, `internal/bootstrap`, `.env.example`.
  - CLI: `recipes list|show|import|run`, `jobs list|show`, `scrape --recipe-id`.

## En curso

Nada.

## Siguiente paso

Empezar la Fase 3 por este orden:

1. Crear `web/` con Vite + React + TypeScript (`npm create vite@latest web -- --template react-ts`) y un
   proxy de desarrollo de `/api` hacia `127.0.0.1:8080` (ver D-013: sin CORS).
2. Cliente tipado de la API (`internal/api` es la referencia; las formas JSON salen de las etiquetas de
   `internal/domain`).
3. Pantalla principal: URL, límites, headless, receta como JSON validado (la instrucción en lenguaje
   natural llega en la Fase 4), inicio del trabajo, progreso con `EventSource` sobre
   `/api/v1/jobs/{id}/events`, vista previa tabular, avisos y errores, botones de exportación.
4. Vistas de historial de trabajos y de recetas (guardar, editar, ejecutar, borrar).
5. Servir `web/dist` desde `cmd/server` (mismo origen) y añadir los comandos al `Makefile`.

## Pendientes conocidos

- Sin protección SSRF ni validación de `Host` (Fase 5): no exponer el servicio fuera de loopback ni
  ejecutar recetas de origen no confiable (D-008, D-013).
- Sin detección de login, CAPTCHA o bloqueo: `domain.ErrBlocked`, el estado `blocked` y el código de salida
  existen, pero nada los emite aún.
- `number` es el único tipo numérico; faltan fechas y moneda (D-006).
- Falta comparar resultados con ejecuciones anteriores ("cambios anormales"); los datos ya se guardan.
- Sin capturas ni trazas de Playwright al fallar, ni política de retención (Fase 5).
- La CLI no tiene modo `--server`; un trabajo en curso solo se cancela desde el proceso que lo ejecuta (D-010).
- No hay endpoint para consultar versiones anteriores de una receta (se guardan en `recipe_versions`).

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
- [x] Fase 2 — Persistencia y servidor: SQLite, migraciones, API HTTP, cola, cancelación, SSE, historial.
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
