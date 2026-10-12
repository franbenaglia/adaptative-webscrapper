# Estado del proyecto

Última actualización: 2026-10-12

## Resumen

Fases 1, 2 y 3 terminadas y verificadas. Hay motor determinista con Playwright, CLI, persistencia SQLite,
cola de trabajos, API HTTP con SSE e interfaz web. Falta la IA (descubrir y reparar recetas) y el
empaquetado y endurecimiento.

## Fase actual

**Fase 4 — IA adaptable**, sin empezar. Requiere instalar Ollama y elegir modelo (ver "Siguiente paso").

## Hecho

- Fase 0: especificación evaluada, documentación de continuidad, Go 1.27.2 instalado, git inicializado.
- Fase 1 (2026-10-12): dominio, motor, adaptador Playwright, exportadores, CLI, sitio de demostración.
- Fase 2 (2026-10-12): estados de trabajo, SQLite, `JobService`, cola, API `/api/v1` con SSE, `cmd/server`.
- Fase 3 (2026-10-12), verificada con `make web-check`, `make check` y una sesión real en navegador contra
  el servidor y el sitio de demostración: extracción con receta JSON, progreso en vivo, tabla de 6
  registros, guardar receta, ejecutar receta guardada, error del servidor mostrado en el formulario,
  editor de recetas con error de sintaxis y de validación, historial.
  - `web/src/api.ts` (cliente tipado), `router.ts` (rutas por hash), `recipe.ts`, `ui.tsx`, `styles.css`.
  - `web/src/views/`: `NewJob`, `JobDetail`, `Jobs`, `Recipes`, `RecipeEditor`.
  - `internal/api`: sirve `APP_WEB_DIR` en la raíz; `Makefile`: `web-install`, `web-build`, `web-dev`, `web-check`.

## En curso

Nada.

## Siguiente paso

Empezar la Fase 4. Antes hay dos decisiones del usuario:

- Instalar Ollama (no está instalado; sin sudo se puede usar el binario en `~/.local` o la imagen Docker).
- Elegir modelo: sin GPU y con 15 GB de RAM, uno pequeño (por ejemplo de 3–4 B de parámetros, unos 2–3 GB
  en disco). Quedan ~19 GB libres.

Después, por este orden:

1. `PageSnapshot` en `internal/domain` y `Snapshot()` en `BrowserSession` (D-004): URL, título, texto visible
   limitado y **candidatos numerados** de contenedores repetidos y de campos, calculados por un script fijo (P-3).
2. Puerto `IntelligenceProvider` (`DiscoverRecipe`, `RepairRecipe`) con una implementación simulada para pruebas.
3. Adaptador Ollama por HTTP con salida estructurada (P-4), decodificación estricta, límite de tamaño y
   `Recipe.Validate()` sobre el resultado. Variables `OLLAMA_BASE_URL`, `OLLAMA_MODEL`, `AI_ENABLED`.
4. Flujo en el motor: sin receta → snapshot → descubrir → validar → probar en la página; si la extracción
   no supera la validación, reparar con un máximo configurable de intentos. Eventos `recipe.discovered`,
   `recipe.validated`, `recipe.repairing`. Criterio de "receta rota" a partir de elementos encontrados y
   campos requeridos ausentes.
5. `Instruction` en `JobRequest`, en la API, en la CLI (`--instruction`) y en el formulario web.
6. Guardar la receta descubierta o reparada como nueva versión (`recipe_versions` ya existe).
7. Páginas de prueba que faltan en `examples/demo`: tabla, cambio de selector y login o bloqueo simulados;
   y detección básica de login/CAPTCHA que emita `domain.ErrBlocked`.

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
- La interfaz web no tiene pruebas automáticas (D-015) ni editor visual de selectores (fuera del MVP).
- La interfaz no tiene campo de instrucción en lenguaje natural hasta la Fase 4.

## Entorno de desarrollo (comprobado el 2026-10-12)

| Herramienta | Estado |
|---|---|
| Go | 1.27.2 en `~/sdk/go1.27.2`, enlazado desde `~/.local/bin/go` y `gofmt` (ya en el PATH). Instalado sin sudo; para actualizar, extraer otra versión en `~/sdk` y reapuntar los enlaces. |
| Node.js | v20.20.2 (suficiente para Vite 8, que pide 20.19 o posterior). `web/node_modules` ocupa 74 MB. |
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
- [x] Fase 3 — Interfaz web: formulario, progreso, vista previa, historial, recetas, exportaciones.
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
