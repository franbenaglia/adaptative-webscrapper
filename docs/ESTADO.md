# Estado del proyecto

Última actualización: 2026-10-12

## Resumen

Fases 1 a 4 terminadas y verificadas. Hay motor determinista con Playwright, CLI, persistencia SQLite, cola
de trabajos, API HTTP con SSE, interfaz web e IA local (descubrir y reparar recetas con Ollama). Falta el
empaquetado y el endurecimiento.

## Fase actual

**Fase 5 — Empaquetado y endurecimiento**, sin empezar.

## Hecho

- Fase 0: especificación evaluada, documentación de continuidad, Go 1.27.2 instalado, git inicializado.
- Fase 1 (2026-10-12): dominio, motor, adaptador Playwright, exportadores, CLI, sitio de demostración.
- Fase 2 (2026-10-12): estados de trabajo, SQLite, `JobService`, cola, API `/api/v1` con SSE, `cmd/server`.
- Fase 3 (2026-10-12): interfaz web en `web/`, servida por `cmd/server`.
- Fase 4 (2026-10-12), verificada con `make check`, `make test-browser`, `make web-check`, `make test-ai`
  (tres pasadas en verde con `llama3.2:3b`) y ejecuciones reales desde la CLI y desde la web:
  - `internal/browser/snapshot.go`: resumen de página en candidatos numerados y detección de bloqueos.
  - `internal/intelligence`: proveedor (prompt, esquema cerrado, traducción a receta) y cliente de Ollama.
  - `internal/application/service.go`: descubrir, validar, probar y reparar (solo en la primera página).
  - `instruction` en dominio, API, CLI (`--instruction`, `--save-recipe`, `ai status`) y web.
  - Migración `0002_ai.sql`; páginas de demostración `tabla.html`, `productos-v2.html`, `login.html`.
  - `docs/GUIA_DE_USO.md`: guía para usuarios finales (mantenerla al día, ver `CLAUDE.md`).

## En curso

Nada.

## Siguiente paso

Empezar la Fase 5 por este orden:

1. **SSRF** (P-5): interceptar todas las peticiones del navegador con `Route`, resolver el host y bloquear
   loopback, link-local, redes privadas y metadatos; revalidar redirecciones; `APP_ALLOW_PRIVATE_NETWORKS`
   para desarrollo (el sitio de demostración vive en `127.0.0.1`, así que las pruebas lo necesitan).
   Bloquear descargas y limitar redirecciones.
2. Validar la cabecera `Host` en la API (DNS rebinding, D-013).
3. Captura de pantalla y traza de Playwright al fallar un trabajo, con retención configurable.
4. `Dockerfile` y `compose.yaml`: binarios, `web/dist`, Chromium y, opcionalmente, un servicio Ollama.
5. Repaso de timeouts y límites; documento breve de arquitectura si el usuario lo quiere aparte de
   `docs/DECISIONES.md`.

## Pendientes conocidos

- Sin protección SSRF ni validación de `Host` (Fase 5): no exponer el servicio fuera de loopback ni
  ejecutar recetas de origen no confiable (D-008, D-013).
- La detección de login y CAPTCHA es heurística (D-018).
- La IA solo se ha probado sobre el sitio de demostración y con un modelo (D-021). No detecta que el modelo
  cruce dos campos del mismo tipo (D-020). Solo repara roturas visibles en la primera página (D-017).
- `number` es el único tipo numérico; faltan fechas y moneda (D-006).
- Falta comparar resultados con ejecuciones anteriores ("cambios anormales"); los datos ya se guardan.
- Sin capturas ni trazas de Playwright al fallar, ni política de retención (Fase 5).
- La CLI no tiene modo `--server`; un trabajo en curso solo se cancela desde el proceso que lo ejecuta (D-010).
- No hay endpoint para consultar versiones anteriores de una receta (se guardan en `recipe_versions`).
- La interfaz web no tiene pruebas automáticas (D-015) ni editor visual de selectores (fuera del MVP).

## Entorno de desarrollo (comprobado el 2026-10-12)

| Herramienta | Estado |
|---|---|
| Go | 1.27.2 en `~/sdk/go1.27.2`, enlazado desde `~/.local/bin/go` y `gofmt` (ya en el PATH). Instalado sin sudo; para actualizar, extraer otra versión en `~/sdk` y reapuntar los enlaces. |
| Node.js | v20.20.2 (suficiente para Vite 8, que pide 20.19 o posterior). `web/node_modules` ocupa 74 MB. |
| Docker / Compose | Instalados (Compose v5.5.1) |
| curl | Es el de snap (`/snap/bin/curl`): no puede escribir en `/tmp` con `-o`; usar redirección `>` del shell. |
| git, make, gcc | Instalados |
| Ollama | v0.40.3 en `~/sdk/ollama-v0.40.3` (solo bibliotecas de CPU, 61 MB), enlazado desde `~/.local/bin/ollama`. No es un servicio: hay que arrancarlo con `ollama serve`. Modelo `llama3.2:3b` descargado en `~/.ollama` (2 GB). |
| Playwright | Driver 1.62.1 en `~/.cache/ms-playwright-go` (134 MB, con su propio Node.js) y Chromium headless en `~/.cache/ms-playwright`. Se reinstala con `make install-browsers`. |
| Hardware | 8 núcleos, 15 GB RAM, sin GPU NVIDIA: la inferencia local será solo por CPU. |
| Disco | 19 GB libres en `/home` (92 % usado). Vigilar: imágenes Docker, navegadores y modelos ocupan varios GB. |

## Fases (de la especificación)

- [x] Fase 1 — Base funcional: módulo Go, dominio, interfaces, CLI mínima, extracción determinista con receta manual, export JSON/CSV, pruebas.
- [x] Fase 2 — Persistencia y servidor: SQLite, migraciones, API HTTP, cola, cancelación, SSE, historial.
- [x] Fase 3 — Interfaz web: formulario, progreso, vista previa, historial, recetas, exportaciones.
- [x] Fase 4 — IA adaptable: adaptador Ollama, snapshots, descubrimiento, validación, reparación, versiones.
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
