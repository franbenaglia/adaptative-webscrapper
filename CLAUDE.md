# AdaptiveScraper — punto de entrada para agentes de IA

Si acabas de llegar sin contexto (sesión nueva, corte o compactación), lee en este orden:

1. `docs/ESTADO.md` — en qué fase estamos, qué está hecho, qué sigue. **Empieza por aquí.**
2. `docs/ARQUITECTURA.md` — mapa de paquetes, recorrido de un trabajo y medidas de seguridad.
3. `docs/DECISIONES.md` — decisiones ya tomadas y su porqué. No las reabras sin motivo nuevo.
4. `Claude_Code_AdaptiveScraper.md` — especificación original del usuario (fuente de verdad del alcance).

`docs/GUIA_DE_USO.md` es la guía para usuarios finales: si cambias comandos, opciones, variables de
configuración, el formato de las recetas o los mensajes que cita, actualízala en el mismo commit.

Después comprueba el estado real antes de fiarte de los documentos: `git status`, `git log --oneline -15`
y la suite de pruebas. Si el código y `docs/ESTADO.md` se contradicen, manda el código; corrige el documento.

## Protocolo de continuidad (obligatorio)

El objetivo es que cualquier agente pueda retomar el trabajo solo con el repositorio.

- **Antes de empezar una tarea**: anótala en "En curso" de `docs/ESTADO.md` con el siguiente paso concreto.
- **Al terminar una unidad de trabajo verificada** (compila y pasan las pruebas): actualiza `docs/ESTADO.md`
  (mueve la tarea a "Hecho", fija el "Siguiente paso") y haz un commit pequeño. El commit es el punto de recuperación.
- **Al tomar una decisión no obvia o desviarte de la especificación**: añade una entrada a `docs/DECISIONES.md`.
- **Si algo queda a medias**: descríbelo en "En curso" con los archivos tocados y qué falta. Nunca dejes
  trabajo a medias sin anotar.
- `docs/ESTADO.md` describe el presente, no es un diario: reescríbelo, no acumules historial (para eso está git).
- No marques nada como hecho sin haber ejecutado la verificación; anota el comando usado.

## Reglas del proyecto (resumen de la especificación)

- Go para backend, motor y CLI; React + TypeScript + Vite para la web; SQLite; Playwright; Ollama opcional.
- Puertos y adaptadores: `internal/domain` e `internal/application` no importan HTTP, Cobra, Playwright,
  SQLite ni Ollama.
- Un único servicio de aplicación compartido por la API y la CLI (`application.JobService`, cableado en
  `internal/bootstrap`). Sin lógica de scraping en `cmd/`, `internal/cli` ni `internal/api`.
- Las migraciones de `migrations/` no se modifican una vez en un commit: los cambios van en un archivo nuevo.
- Las recetas son declarativas. La salida del modelo es entrada no confiable: nunca se ejecuta código generado.
- Sin evasión de CAPTCHA, anti-bot ni login: ante un bloqueo el trabajo termina en estado `blocked`.
- La suite normal de pruebas no depende de Internet, Ollama ni sitios de terceros.
- Trabajo incremental por fases; el repositorio queda ejecutable al final de cada una.

## Comandos

- `make check` — `go vet`, pruebas rápidas con `-race` y comprobación de formato. Ejecútalo antes de cada commit.
- `make test-browser` — pruebas de integración con Chromium real (build tag `browser`).
- `make test-ai` — recorrido con un modelo real; requiere `ollama serve` y el modelo. Ejecútalo varias
  veces tras tocar `internal/intelligence/provider.go` (D-021).
- `make build` — genera `bin/adaptive-scraper`, `bin/adaptive-scraper-server` y `bin/demosite`.
- `make server` — API HTTP en `127.0.0.1:8080`; los datos van a `data/` (ignorado por git).
- `make demo-site` y, en otra terminal, `make demo` — ejecución completa contra el sitio local.
- `make install-browsers` — instala el driver de Playwright y Chromium (una vez por versión).
- `make fmt` — aplica `gofmt`.
- `make web-install` (una vez), `make web-check` (tipos + compilación) y `make web-build` (genera `web/dist`,
  que `make server` publica en la raíz). `make web-dev` arranca Vite con proxy hacia el servidor.

El módulo Go se llama `adaptive-scraper`; Go está en `~/.local/bin/go`.

El sitio de demostración y los servidores de prueba viven en `127.0.0.1`, que la política de red bloquea
por defecto: para ejecutar binarios contra ellos hace falta `APP_ALLOW_PRIVATE_NETWORKS=true` (el `.env`
local del usuario ya lo tiene; las pruebas usan `browser.Options` con `AllowPrivate`).
