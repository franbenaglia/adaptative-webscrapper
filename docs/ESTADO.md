# Estado del proyecto

Última actualización: 2026-10-12

## Resumen

Entorno listo y repositorio git inicializado. Solo existen la especificación y estos documentos de
continuidad. No hay código ni módulo Go todavía.

## Fase actual

**Fase 0 — Preparación, terminada.** Lo siguiente es la Fase 1.

## Hecho

- 2026-10-12: leída la especificación y evaluada la factibilidad (conclusión: factible; riesgos en
  `docs/DECISIONES.md`, sección "Riesgos abiertos").
- 2026-10-12: creados `CLAUDE.md`, `AGENTS.md`, `docs/ESTADO.md`, `docs/DECISIONES.md`.
- 2026-10-12: instalado Go 1.27.2 sin sudo (checksum verificado; `go version` responde).
- 2026-10-12: `git init` y primer commit.

## En curso

Nada.

## Siguiente paso

1. Presentar al usuario el plan corto de la Fase 1 (decisiones y riesgos) y empezar a implementarla.
2. Primer paso técnico: `go mod init`, verificar la ruta vigente de `playwright-go` (P-1 en
   `docs/DECISIONES.md`) y definir `internal/domain`.

## Entorno de desarrollo (comprobado el 2026-10-12)

| Herramienta | Estado |
|---|---|
| Go | 1.27.2 en `~/sdk/go1.27.2`, enlazado desde `~/.local/bin/go` y `gofmt` (ya en el PATH). Instalado sin sudo; para actualizar, extraer otra versión en `~/sdk` y reapuntar los enlaces. |
| Node.js | v20.20.2 |
| Docker / Compose | Instalados (Compose v5.5.1) |
| curl | Es el de snap (`/snap/bin/curl`): no puede escribir en `/tmp` con `-o`; usar redirección `>` del shell. |
| git, make, gcc | Instalados |
| Ollama | **No instalado**. Solo hace falta desde la Fase 4. |
| Navegadores Playwright | Chromium en `~/.cache/ms-playwright` (builds 1223 y 1234); puede no coincidir con la versión que exija `playwright-go`. |
| Hardware | 8 núcleos, 15 GB RAM, sin GPU NVIDIA: la inferencia local será solo por CPU. |
| Disco | 19 GB libres en `/home` (92 % usado). Vigilar: imágenes Docker, navegadores y modelos ocupan varios GB. |

## Fases (de la especificación)

- [ ] Fase 1 — Base funcional: módulo Go, dominio, interfaces, CLI mínima, extracción determinista con receta manual, export JSON/CSV, pruebas.
- [ ] Fase 2 — Persistencia y servidor: SQLite, migraciones, API HTTP, cola, cancelación, SSE, historial.
- [ ] Fase 3 — Interfaz web: formulario, progreso, vista previa, historial, recetas, exportaciones.
- [ ] Fase 4 — IA adaptable: adaptador Ollama, snapshots, descubrimiento, validación, reparación, versiones.
- [ ] Fase 5 — Empaquetado y endurecimiento: Docker, SSRF completo, timeouts, trazas, documentación.

### Criterios de aceptación del primer hito (Fase 1)

- [ ] Ejecutar una página HTML de ejemplo local.
- [ ] Ejecutar la CLI con una receta manual.
- [ ] Extraer nombre, precio y disponibilidad mediante Playwright.
- [ ] Recorrer dos páginas mediante un botón siguiente.
- [ ] Limitar páginas y registros.
- [ ] Exportar los mismos resultados en JSON y CSV.
- [ ] Ejecutar las pruebas sin conexión a Internet.
- [ ] Probar que la CLI usa el mismo servicio de aplicación que usará la API.
