# Estado del proyecto

Última actualización: 2026-10-12

## Resumen

Las cinco fases del MVP están implementadas: motor determinista con Playwright, CLI, persistencia SQLite,
cola de trabajos, API HTTP con SSE, interfaz web, IA local (descubrir y reparar recetas con Ollama),
política de red, diagnósticos y empaquetado Docker.

## Fase actual

**Fase 5 — Empaquetado y endurecimiento**: código y documentación terminados. Ver "En curso" para lo que
falta verificar de Docker.

## Hecho

- Fase 0: especificación evaluada, documentación de continuidad, Go 1.27.2 instalado, git inicializado.
- Fase 1 (2026-10-12): dominio, motor, adaptador Playwright, exportadores, CLI, sitio de demostración.
- Fase 2 (2026-10-12): estados de trabajo, SQLite, `JobService`, cola, API `/api/v1` con SSE, `cmd/server`.
- Fase 3 (2026-10-12): interfaz web en `web/`, servida por `cmd/server`.
- Fase 4 (2026-10-12): resumen de página en candidatos, proveedor de IA, Ollama, descubrir y reparar,
  detección de bloqueos. Verificado con `llama3.2:3b`.
- Fase 5 (2026-10-12), verificada con `make check`, `make test-browser` y ejecuciones reales de los binarios
  (bloqueo de 127.0.0.1 por defecto, extracción de un sitio HTTPS público a través del proxy, captura y
  traza de un trabajo bloqueado servidas por la API, rechazo de `Host` ajeno):
  - `internal/netguard`: política de red y proxy de filtrado; Chromium lo usa para todo su tráfico (D-022).
  - `internal/api`: validación de `Host` (D-023) y endpoints de diagnósticos.
  - Capturas y trazas al fallar, con retención (D-024); la web las muestra en el detalle del trabajo.
  - `Dockerfile`, `compose.yaml`, `.dockerignore` (D-025).
  - `docs/ARQUITECTURA.md`.

## En curso

- Verificar la imagen Docker: `docker build -t adaptive-scraper:latest .` y después arrancar con
  `docker compose up -d`, comprobar `/healthz`, la interfaz y una extracción real. Si esta nota sigue aquí,
  la imagen no se ha comprobado de extremo a extremo.

## Siguiente paso

El MVP de la especificación está completo. Antes de añadir nada, pregunta al usuario qué quiere priorizar.
Candidatos, de más a menos valioso según lo aprendido:

1. Probar la IA en sitios reales y con otros modelos; ajustar el resumen de página (`snapshotScript`) con
   lo que falle.
2. Modo `--server` de la CLI (enviar el trabajo a un servidor en marcha).
3. Reparación cuando la rotura aparece en páginas posteriores a la primera.
4. Tipos de campo `date` y `money`; comparación con ejecuciones anteriores ("cambios anormales").
5. Otras paginaciones (scroll infinito, enlaces numerados) y páginas de detalle.
6. Pruebas automáticas de la interfaz web; endpoint para consultar versiones anteriores de una receta.
7. Autenticación en la API si alguna vez se publica fuera de loopback.

## Limitaciones conocidas

- La API no tiene autenticación (D-013, D-023): solo para uso en loopback.
- La IA solo se ha probado sobre el sitio de demostración y con un modelo (D-021). No detecta que el modelo
  cruce dos campos del mismo tipo (D-020). Solo repara roturas visibles en la primera página (D-017).
- La detección de login y CAPTCHA es heurística (D-018).
- El límite de tamaño de respuesta es por conexión, no por recurso (D-022).
- No hay diagnóstico tras una cancelación o un tiempo máximo; la CLI no aplica la retención (D-024).
- Solo listados con paginación por botón: sin páginas de detalle, scroll infinito ni iframes (R-5).
- `number` es el único tipo numérico; faltan fechas y moneda (D-006). No se comparan resultados con
  ejecuciones anteriores.
- La CLI no tiene modo `--server`; un trabajo en curso solo se cancela desde el proceso que lo ejecuta (D-010).
- La interfaz web no tiene pruebas automáticas (D-015). No hay endpoint para versiones anteriores de recetas.
- El servicio Ollama de `compose.yaml` (perfil `ai`) no se ha probado.

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
