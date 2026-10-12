# Prompt para Claude Code — AdaptiveScraper

Quiero que desarrolles una aplicación local de extracción web general asistida por IA. El nombre provisional del proyecto es **AdaptiveScraper**.

La aplicación debe tener:

1. Una interfaz web para usuarios.
2. Un backend escrito en Go.
3. Una herramienta de línea de comandos escrita en Go.
4. Un único motor de scraping reutilizado tanto por la API web como por la CLI.
5. Automatización del navegador mediante Playwright.
6. Integración opcional con un modelo pequeño de lenguaje, preferentemente local mediante Ollama.

El objetivo no es que la IA controle cada acción de cada página. La IA debe utilizarse principalmente para descubrir una receta de extracción inicial y para repararla cuando la estructura del sitio cambie. Cuando exista una receta válida, las ejecuciones normales deben ser deterministas y no deben invocar al modelo innecesariamente.

## Principios de arquitectura

Usa una arquitectura modular inspirada en puertos y adaptadores. El dominio y los casos de uso no deben depender de HTTP, Cobra, Playwright, SQLite ni Ollama.

La interfaz web y la CLI deben invocar el mismo servicio de aplicación. No dupliques lógica de scraping en `cmd/server`, los handlers HTTP o los comandos de Cobra.

Las dependencias externas deben estar detrás de interfaces, especialmente:

- Control del navegador.
- Proveedor de inteligencia artificial.
- Repositorio de recetas.
- Repositorio de trabajos y resultados.
- Exportadores.
- Reloj o generador de identificadores cuando sea útil para las pruebas.

Estructura inicial sugerida:

```text
adaptive-scraper/
├── cmd/
│   ├── server/
│   │   └── main.go
│   └── scraper/
│       └── main.go
├── internal/
│   ├── domain/
│   ├── application/
│   ├── browser/
│   ├── intelligence/
│   ├── recipes/
│   ├── jobs/
│   ├── storage/
│   ├── export/
│   └── api/
├── web/
├── migrations/
├── examples/
├── Dockerfile
├── compose.yaml
├── Makefile
├── go.mod
└── README.md
```

Puedes ajustar esta estructura si existe una razón técnica clara, pero mantén la separación entre dominio, casos de uso e infraestructura.

## Tecnologías preferidas

- Go para el backend, el motor y la CLI.
- React, TypeScript y Vite para la interfaz web.
- Cobra para la CLI.
- SQLite para persistencia local.
- `log/slog` para logs estructurados.
- Server-Sent Events para mostrar el progreso de los trabajos en la web.
- Playwright para Go mediante `github.com/mxschmitt/playwright-go`, encapsulado detrás de una interfaz propia.
- Ollama mediante HTTP como primer proveedor local de IA.
- Docker Compose para facilitar la ejecución del sistema completo.

No acoples el dominio directamente a `playwright-go`. Debe ser posible reemplazar en el futuro el adaptador Go por un worker de Playwright en Node.js sin modificar la API, la CLI ni los casos de uso.

## Alcance del MVP

El MVP debe permitir:

1. Introducir una URL inicial.
2. Escribir en lenguaje natural qué datos se desean extraer.
3. Limitar la cantidad máxima de páginas y registros.
4. Abrir la página con Playwright.
5. Obtener una representación reducida y segura de la página: URL, título, texto relevante, elementos interactivos, fragmentos de DOM y, cuando sea posible, información del árbol de accesibilidad.
6. Pedir al modelo una receta estructurada de extracción.
7. Validar estrictamente la respuesta del modelo antes de ejecutarla.
8. Probar la receta sobre la primera página.
9. Mostrar una vista previa de los registros detectados.
10. Recorrer paginación mediante botón “siguiente” en el MVP.
11. Exportar resultados a JSON y CSV.
12. Guardar la receta para volver a ejecutarla sin usar IA.
13. Detectar que una receta dejó de funcionar e intentar repararla con IA.
14. Guardar historial, estado, errores y cantidad de registros de cada ejecución.

El MVP no debe intentar resolver CAPTCHA, evadir sistemas anti-bot ni eludir controles de acceso. Si detecta CAPTCHA, autenticación obligatoria, bloqueo o verificación humana, debe detener el trabajo con un estado y mensaje claros.

## Modelo de dominio mínimo

Define tipos equivalentes a los siguientes, ajustándolos cuando sea necesario:

```go
type ScrapeRequest struct {
    URL         string
    Instruction string
    RecipeID    string
    MaxPages    int
    MaxRecords  int
    Headless    bool
}

type Record map[string]any

type ScrapeResult struct {
    JobID       string
    Fields      []FieldDefinition
    Records     []Record
    PagesVisited int
    Warnings    []string
}

type FieldDefinition struct {
    Name       string
    Type       string
    Locator    string
    Attribute  string
    Required   bool
}

type Recipe struct {
    ID            string
    Name          string
    StartURL      string
    AllowedHosts  []string
    ItemLocator   string
    Fields        []FieldDefinition
    Pagination    PaginationRule
    Version       int
}
```

Una receta debe ser declarativa. No permitas que el modelo genere JavaScript arbitrario, comandos del sistema ni código Go que luego sea ejecutado.

## Interfaces principales

Crea contratos pequeños y fáciles de simular en pruebas. Por ejemplo:

```go
type Browser interface {
    Start(ctx context.Context, options BrowserOptions) error
    Navigate(ctx context.Context, rawURL string) error
    Snapshot(ctx context.Context) (PageSnapshot, error)
    Extract(ctx context.Context, recipe Recipe) ([]Record, error)
    GoToNextPage(ctx context.Context, rule PaginationRule) (bool, error)
    Screenshot(ctx context.Context) ([]byte, error)
    Close() error
}

type IntelligenceProvider interface {
    DiscoverRecipe(ctx context.Context, snapshot PageSnapshot, instruction string) (Recipe, error)
    RepairRecipe(ctx context.Context, previous Recipe, snapshot PageSnapshot, failure ExtractionFailure) (Recipe, error)
}

type ScrapingService interface {
    Run(ctx context.Context, request ScrapeRequest) (ScrapeResult, error)
}
```

No copies estas interfaces mecánicamente si detectas una división mejor. Evita interfaces excesivamente grandes.

## Flujo de extracción

Implementa el siguiente comportamiento:

1. Validar la URL y aceptar inicialmente solo `http` y `https`.
2. Crear un trabajo en estado `queued`.
3. Pasarlo a `running` cuando haya capacidad disponible.
4. Abrir un contexto de navegador aislado.
5. Navegar a la URL respetando timeouts y cancelación mediante `context.Context`.
6. Detectar errores, páginas de login, CAPTCHA o bloqueos.
7. Si se proporcionó una receta, probarla primero sin llamar a la IA.
8. Si no existe receta, crear un snapshot reducido y solicitar una al modelo.
9. Validar la receta y probarla en la página actual.
10. Si la extracción no supera la validación, permitir como máximo una cantidad configurable y pequeña de intentos de reparación.
11. Recorrer páginas hasta alcanzar `MaxPages`, `MaxRecords`, no encontrar una siguiente página o recibir una cancelación.
12. Persistir resultados de manera incremental para evitar perder todo el trabajo ante un fallo tardío.
13. Cerrar siempre página, contexto y navegador.
14. Finalizar como `completed`, `failed`, `blocked` o `cancelled`.

Evita esperas fijas cuando Playwright pueda esperar condiciones concretas. Agrega límites de tiempo a navegación, acciones y trabajo total.

## Representación enviada a la IA

No envíes el HTML completo de forma predeterminada. Construye un `PageSnapshot` compacto que contenga:

- URL y título.
- Texto visible normalizado y limitado.
- Estructura resumida de elementos repetidos.
- Enlaces y botones relevantes.
- Roles, nombres accesibles y atributos estables.
- Candidatos de listas, tablas o tarjetas repetidas.
- Mensajes de error de la receta anterior cuando se trate de una reparación.

El prompt del proveedor debe exigir JSON válido conforme a un esquema conocido. Usa decodificación estricta, límites de tamaño y validaciones semánticas. El modelo no es una fuente confiable: todo resultado debe considerarse entrada no confiable.

## Validación de recetas y resultados

Antes de guardar o ejecutar una receta, valida como mínimo:

- Nombres de campos únicos y no vacíos.
- Tipos de campo permitidos.
- Selectores con longitud limitada.
- Ausencia de scripts o esquemas de URL peligrosos.
- Host de navegación dentro de `AllowedHosts`.
- Cantidad máxima de campos.
- Regla de paginación válida.

Después de extraer, evalúa:

- Porcentaje de campos requeridos presentes.
- Cantidad de elementos encontrados.
- Registros duplicados.
- Conversión de números, fechas y valores monetarios.
- Cambios anormales respecto de ejecuciones anteriores.

Guarda advertencias sin descartar automáticamente datos potencialmente útiles.

## Seguridad de navegación

Como la URL proviene del usuario, agrega protección básica contra SSRF:

- Bloquear por defecto `localhost`, loopback, link-local, redes privadas y endpoints de metadatos.
- Resolver y comprobar DNS antes de navegar.
- Volver a validar redirecciones y hosts secundarios.
- Permitir una opción explícita de desarrollo para autorizar hosts locales.
- Restringir protocolos a HTTP y HTTPS.
- Limitar descargas, tamaños de respuesta, redirecciones y duración.

No implementes evasión de CAPTCHA, rotación furtiva de fingerprints, bypass de login ni técnicas destinadas a eludir protecciones. Agrega una nota en la documentación indicando que el usuario es responsable de respetar condiciones de uso, robots, derechos sobre datos y límites de acceso.

## Persistencia

Usa SQLite y migraciones versionadas. Como mínimo, contempla:

- `recipes`
- `recipe_versions`
- `jobs`
- `job_events`
- `records`

Los estados del trabajo deben tener transiciones explícitas. No guardes información sensible en logs. Prepara la estructura para almacenar sesiones en el futuro, pero no incluyas gestión de credenciales en el primer MVP.

## API HTTP

Diseña una API JSON versionada. Como mínimo:

```text
POST   /api/v1/jobs
GET    /api/v1/jobs
GET    /api/v1/jobs/{id}
POST   /api/v1/jobs/{id}/cancel
GET    /api/v1/jobs/{id}/records
GET    /api/v1/jobs/{id}/events
GET    /api/v1/jobs/{id}/export?format=csv|json

GET    /api/v1/recipes
POST   /api/v1/recipes
GET    /api/v1/recipes/{id}
PUT    /api/v1/recipes/{id}
DELETE /api/v1/recipes/{id}
POST   /api/v1/recipes/{id}/run
```

`/events` debe usar Server-Sent Events y enviar eventos de progreso como:

- `job.started`
- `page.opened`
- `recipe.discovered`
- `recipe.validated`
- `records.extracted`
- `recipe.repairing`
- `job.completed`
- `job.failed`
- `job.blocked`

Los handlers deben ser delgados y delegar en servicios de aplicación.

## CLI

El binario provisional puede llamarse `adaptive-scraper`. Debe ofrecer:

```bash
adaptive-scraper scrape \
  --url "https://example.com/products" \
  --instruction "Extraer nombre, precio y disponibilidad" \
  --max-pages 5 \
  --max-records 200 \
  --output products.csv
```

Comandos previstos:

```text
adaptive-scraper scrape
adaptive-scraper recipes list
adaptive-scraper recipes show <id>
adaptive-scraper recipes run <id>
adaptive-scraper jobs list
adaptive-scraper jobs show <id>
adaptive-scraper version
```

En el MVP, `scrape` debe funcionar en modo local invocando directamente el motor compartido. Diseña los contratos para poder incorporar posteriormente:

```bash
adaptive-scraper scrape ... --server http://localhost:8080
```

Ese modo futuro enviará el trabajo a un servidor ya iniciado. No dupliques reglas de validación entre los dos modos.

La CLI debe:

- Escribir progreso legible en stderr.
- Escribir datos en stdout cuando no se indica `--output`.
- Ofrecer salida JSON para automatizaciones.
- Utilizar códigos de salida diferentes para error general, bloqueo, cancelación y receta inválida.

## Interfaz web

Crea una interfaz sobria, adaptable a escritorio y utilizable localmente. El flujo principal debe contener:

1. URL.
2. Instrucción de extracción.
3. Límites de páginas y registros.
4. Opción headless.
5. Inicio del trabajo.
6. Progreso en tiempo real.
7. Vista previa tabular.
8. Advertencias y errores claros.
9. Botones de exportación JSON y CSV.
10. Posibilidad de guardar y reutilizar la receta.

Incluye vistas para historial de trabajos y recetas. No intentes construir un editor visual complejo de selectores en la primera versión; muestra la receta como formulario sencillo o JSON validado.

## Cola y concurrencia

Implementa una cola de trabajos en proceso, inicialmente dentro del servidor. La cantidad máxima de navegadores simultáneos debe ser configurable y tener un valor conservador por defecto.

Cada trabajo debe tener su propio contexto de navegador. La cancelación debe propagarse mediante `context.Context`. Un panic o fallo de un trabajo no debe derribar todo el servidor.

No es necesario usar Redis ni un broker externo para el MVP. Define interfaces que permitan cambiar la implementación más adelante.

## Configuración

Admite variables de entorno y un archivo local opcional. Incluye al menos:

```text
APP_HTTP_ADDR
APP_DATABASE_PATH
APP_DATA_DIR
APP_MAX_CONCURRENT_JOBS
APP_ALLOW_PRIVATE_NETWORKS
PLAYWRIGHT_HEADLESS
OLLAMA_BASE_URL
OLLAMA_MODEL
AI_ENABLED
```

Incluye `.env.example`, pero nunca guardes secretos reales.

## Pruebas

Escribe pruebas desde el principio:

- Pruebas unitarias del servicio de scraping usando navegadores y proveedores de IA simulados.
- Pruebas de validación de recetas.
- Pruebas de transiciones de estado.
- Pruebas de exportación CSV y JSON.
- Pruebas de protección SSRF.
- Pruebas de repositorios SQLite con base temporal.
- Una prueba de integración opcional con Playwright sobre un servidor HTTP local controlado.

No hagas que la suite normal dependa de Internet, Ollama ni sitios de terceros. Separa las pruebas lentas o de navegador mediante build tags o comandos específicos.

Crea páginas HTML de prueba locales que cubran:

- Tarjetas de productos.
- Tabla.
- Botón siguiente.
- Campos faltantes.
- Cambio de selector.
- Página de login o bloqueo simulada.

## Observabilidad y diagnóstico

- Usa logs estructurados con identificadores de trabajo.
- Registra duración por página y cantidad de registros, pero evita almacenar el contenido completo de páginas en logs.
- Conserva opcionalmente una captura y una traza de Playwright cuando un trabajo falle.
- Aplica una política de retención configurable para capturas y trazas.
- Expón un endpoint de salud.

## Entregables

Quiero que trabajes de manera incremental y dejes el repositorio ejecutable en cada etapa. Entrega:

1. Código fuente completo.
2. Migraciones SQLite.
3. Pruebas automatizadas.
4. README con instalación y ejemplos.
5. Dockerfile y `compose.yaml`.
6. Makefile con comandos habituales.
7. Ejemplos de recetas.
8. Página HTML local de demostración para probar el scraper sin depender de terceros.
9. Documento breve de decisiones arquitectónicas.

## Fases de implementación

Antes de escribir código, inspecciona el repositorio y resume lo encontrado. Si el repositorio está vacío, comienza con estas fases:

### Fase 1 — Base funcional

- Inicializar módulo Go y frontend.
- Definir dominio, interfaces y configuración.
- Implementar CLI mínima.
- Implementar extracción determinista con una receta creada manualmente.
- Exportar JSON y CSV.
- Añadir pruebas unitarias.

### Fase 2 — Persistencia y servidor

- SQLite y migraciones.
- API HTTP.
- Cola de trabajos.
- Cancelación y SSE.
- Historial de ejecuciones.

### Fase 3 — Interfaz web

- Formulario de extracción.
- Progreso.
- Vista previa de resultados.
- Historial y recetas.
- Descarga de exportaciones.

### Fase 4 — IA adaptable

- Adaptador Ollama.
- Creación de snapshots reducidos.
- Descubrimiento de recetas.
- Validación estricta.
- Reparación limitada de recetas.
- Registro de versiones.

### Fase 5 — Empaquetado y endurecimiento

- Docker.
- Protección SSRF completa.
- Timeouts y límites.
- Trazas de diagnóstico.
- Documentación y ejemplos.

## Forma de trabajo solicitada

- Primero presenta un plan corto y señala decisiones o riesgos importantes.
- Después implementa la Fase 1; no generes todo como una masa de archivos sin verificar.
- Ejecuta formateadores, pruebas y análisis estático después de cada etapa.
- Si una librería o API difiere de lo esperado, comprueba su documentación o el código instalado antes de inventar métodos.
- Mantén los cambios pequeños y coherentes.
- No reemplaces código correcto sin necesidad.
- Evita abstracciones prematuras, pero respeta los límites entre dominio e infraestructura.
- Explica brevemente cualquier desviación respecto de esta especificación.
- Cuando encuentres una ambigüedad que no bloquee el MVP, toma una decisión razonable, documéntala y continúa.

## Criterios de aceptación del primer hito

El primer hito se considera terminado cuando sea posible:

1. Ejecutar una página HTML de ejemplo local.
2. Ejecutar la CLI con una receta manual.
3. Extraer al menos nombre, precio y disponibilidad a través de Playwright.
4. Recorrer dos páginas mediante un botón siguiente.
5. Limitar páginas y registros.
6. Exportar los mismos resultados en JSON y CSV.
7. Ejecutar las pruebas sin conexión a Internet.
8. Demostrar mediante una prueba que la CLI utiliza el mismo servicio de aplicación que utilizará la API.

Comienza inspeccionando el repositorio actual. Luego propón el plan concreto de la Fase 1 y continúa con su implementación, salvo que exista un bloqueo técnico real.
