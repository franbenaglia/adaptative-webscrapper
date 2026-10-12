# Guía de uso de AdaptiveScraper

Esta guía te lleva desde la instalación hasta tu primera extracción y explica cómo escribir, guardar y
reutilizar recetas. No hace falta saber programar, pero sí usar una terminal y, para escribir recetas a
mano, reconocer un selector CSS.

## Contenido

1. [Qué hace la aplicación](#1-qué-hace-la-aplicación)
2. [Instalación](#2-instalación)
3. [Primera extracción](#3-primera-extracción)
4. [Configuración](#4-configuración)
5. [Recetas](#5-recetas)
6. [Extraer con IA, sin escribir la receta](#6-extraer-con-ia-sin-escribir-la-receta)
7. [Resultados, estados y avisos](#7-resultados-estados-y-avisos)
8. [Problemas frecuentes](#8-problemas-frecuentes)
9. [Límites y uso responsable](#9-límites-y-uso-responsable)

## 1. Qué hace la aplicación

AdaptiveScraper abre una página web en un navegador real, localiza los elementos que se repiten (productos,
filas de una tabla, anuncios) y guarda los datos de cada uno en CSV o JSON. Puede pasar de página pulsando el
botón «siguiente».

Lo que debe extraer se describe en una **receta**: un archivo pequeño que dice dónde está cada dato. Hay dos
formas de conseguir una:

- **Escribirla tú**, con selectores CSS. Es la forma más fiable y no necesita nada más.
- **Pedírsela a la IA**: describes los datos en una frase y un modelo local propone la receta. Es opcional y
  requiere instalar Ollama.

Una vez que tienes una receta, ejecutarla es siempre igual de rápido y no usa IA.

La aplicación se usa de tres maneras, que comparten las mismas recetas y el mismo historial:

| Forma | Para qué |
|---|---|
| Interfaz web | Uso diario: formularios, progreso en vivo y vista previa. |
| Línea de comandos | Automatizar y encadenar con otros programas. |
| API HTTP | Integrarla en otra aplicación. Está descrita en el [README](../README.md). |

## 2. Instalación

### Requisitos

| Programa | Versión | Para qué |
|---|---|---|
| Go | 1.27 o posterior | Compilar la aplicación. |
| Node.js | 20.19 o posterior | Compilar la interfaz web. |
| make | cualquiera | Ejecutar los pasos de esta guía. |
| Ollama | reciente | Solo si quieres la IA (sección 6). |

Hace falta conexión a Internet durante la instalación. Después, la aplicación solo se conecta a los sitios
que le pidas extraer.

### Pasos

Desde la carpeta del proyecto:

```bash
make install-browsers   # descarga el navegador que usa la aplicación (una sola vez)
make build              # compila los programas en bin/
make web-install        # descarga las dependencias de la interfaz web (una sola vez)
make web-build          # compila la interfaz web
```

Al terminar tendrás:

| Archivo | Qué es |
|---|---|
| `bin/adaptive-scraper` | La línea de comandos. |
| `bin/adaptive-scraper-server` | El servidor: interfaz web y API. |
| `bin/demosite` | Un sitio de ejemplo para practicar sin tocar webs reales. |

Comprueba la instalación:

```bash
bin/adaptive-scraper version
```

## 3. Primera extracción

El proyecto incluye un sitio de demostración con una tienda ficticia. Ábrelo en una terminal y déjalo en
marcha:

```bash
make demo-site          # sirve el sitio en http://127.0.0.1:8090
```

### Desde la línea de comandos

En otra terminal:

```bash
bin/adaptive-scraper scrape \
  --recipe examples/recipes/productos-demo.json \
  --output productos.csv
```

Verás el progreso y, al final, un resumen:

```text
página 1: http://127.0.0.1:8090/productos.html
página 1: 3 registros acumulados
página 2: http://127.0.0.1:8090/productos-2.html
página 2: 6 registros acumulados
aviso: campo requerido "disponibilidad" ausente en 1 de 6 registros
6 registros en 2 páginas (completed)
```

El archivo `productos.csv` contiene los seis productos. El aviso es intencionado: uno de los productos de
ejemplo no tiene disponibilidad.

### Desde la interfaz web

```bash
make server
```

Abre `http://127.0.0.1:8080` en tu navegador.

1. En **Nueva extracción** elige «Receta en JSON».
2. Pega el contenido de `examples/recipes/productos-demo.json`.
3. Pulsa **Iniciar extracción**.

Verás el progreso en vivo, la tabla con los registros y los botones para exportar. Con **Guardar receta** la
receta queda disponible en la sección **Recetas** para volver a usarla.

## 4. Configuración

La configuración se hace con variables de entorno o, más cómodo, con un archivo `.env` en la carpeta del
proyecto:

```bash
cp .env.example .env    # y edita .env
```

Todas las variables son opcionales.

| Variable | Valor por defecto | Qué controla |
|---|---|---|
| `APP_HTTP_ADDR` | `127.0.0.1:8080` | Dónde escucha el servidor. |
| `APP_DATA_DIR` | `data` | Carpeta donde se guardan los datos. |
| `APP_DATABASE_PATH` | `data/adaptive-scraper.db` | Archivo con recetas e historial. |
| `APP_MAX_CONCURRENT_JOBS` | `2` | Cuántas extracciones ejecuta el servidor a la vez. |
| `APP_JOB_TIMEOUT` | `10m` | Tiempo máximo de una extracción. |
| `PLAYWRIGHT_HEADLESS` | `true` | `false` muestra la ventana del navegador mientras trabaja. |
| `APP_WEB_DIR` | `web/dist` | Dónde está la interfaz web compilada. |
| `AI_ENABLED` | `false` | Activa la IA (sección 6). |
| `OLLAMA_BASE_URL` | `http://127.0.0.1:11434` | Dónde está Ollama. |
| `OLLAMA_MODEL` | `llama3.2:3b` | Qué modelo usa la IA. |
| `AI_MAX_REPAIRS` | `2` | Cuántas veces intenta reparar una receta rota. |
| `AI_TIMEOUT` | `5m` | Tiempo máximo de cada consulta al modelo. |

Tres cosas que conviene saber:

- **Los datos se guardan en la carpeta desde la que ejecutas el programa** (`data/`, salvo que la cambies).
  Si lanzas la línea de comandos y el servidor desde la misma carpeta, comparten recetas e historial. Si los
  lanzas desde carpetas distintas, no. Para evitar sorpresas, pon una ruta absoluta en `APP_DATA_DIR`.
- **El servidor no pide contraseña.** Por eso solo escucha en tu propio equipo. No cambies `APP_HTTP_ADDR` a
  una dirección accesible desde la red.
- **Ver el navegador ayuda a depurar.** Con `PLAYWRIGHT_HEADLESS=false`, o `--headless=false` en un comando
  concreto, verás lo que hace.

## 5. Recetas

### Qué contiene una receta

Esta es la receta del sitio de demostración:

```json
{
  "name": "Productos de demostración",
  "start_url": "http://127.0.0.1:8090/productos.html",
  "item_locator": "article.product",
  "fields": [
    {"name": "nombre", "type": "text", "locator": ".name", "required": true},
    {"name": "precio", "type": "number", "locator": ".price", "required": true},
    {"name": "disponibilidad", "type": "text", "locator": ".stock", "required": true},
    {"name": "enlace", "type": "url", "locator": ".name a", "attribute": "href"}
  ],
  "pagination": {"type": "next_button", "next_locator": "button.next"}
}
```

| Clave | Obligatoria | Significado |
|---|---|---|
| `name` | Sí | Nombre para reconocerla. |
| `start_url` | Sí | Página donde empieza. Debe ser `http` o `https`. |
| `item_locator` | Sí | Selector CSS de **cada elemento repetido**: una tarjeta, una fila. Cada coincidencia produce un registro. |
| `fields` | Sí | Los datos de cada elemento (entre 1 y 50). |
| `pagination` | No | Cómo pasar de página. Sin ella se lee solo la primera. |
| `allowed_hosts` | No | Sitios a los que puede llegar la navegación. Por defecto, solo el de `start_url`. |

Cada campo de `fields`:

| Clave | Obligatoria | Significado |
|---|---|---|
| `name` | Sí | Nombre de la columna. Letras sin acentos, números y `_`; no puede empezar por número. |
| `type` | Sí | `text`, `number` o `url`. |
| `locator` | No | Selector CSS **dentro del elemento**. Si se omite, se usa el propio elemento. |
| `attribute` | No | Atributo a leer, como `href` o `src`. Si se omite, se lee el texto visible. |
| `required` | No | `true` si todo registro debería tenerlo. Si falta, recibirás un aviso. |

Cómo se interpreta cada tipo:

| Tipo | Resultado |
|---|---|
| `text` | El texto tal cual, sin espacios sobrantes. |
| `number` | El primer número del texto. Entiende `$ 1.234,50` y `1,234.50 USD`. Si no hay número, queda vacío. |
| `url` | La dirección completa, aunque en la página esté escrita como `/producto/1`. |

Un detalle del tipo `number`: un único punto o coma seguido de exactamente tres cifras se toma como
separador de miles, así que `1.234` se lee como 1234.

`pagination` admite dos formas:

```json
{"type": "none"}
{"type": "next_button", "next_locator": "button.next"}
```

Con `next_button`, la aplicación pulsa ese control después de cada página. Se detiene cuando el botón no
existe, está oculto o deshabilitado, cuando el contenido deja de cambiar, o al llegar a los límites de
páginas o registros.

### Cómo escribir una receta para tu sitio

1. Abre la página en tu navegador y pulsa con el botón derecho sobre uno de los elementos que quieres
   extraer; elige **Inspeccionar**.
2. **Encuentra el contenedor que se repite.** Sube por el código hasta dar con la etiqueta que envuelve un
   elemento completo y que aparece una vez por elemento. Su selector es `item_locator`. Por ejemplo, si cada
   producto es `<article class="product">`, el selector es `article.product`.
3. **Localiza cada dato dentro de ese contenedor.** Si el nombre está en `<h2 class="name">`, el `locator`
   del campo es `.name`. Los selectores de los campos se buscan dentro de cada elemento, no en toda la página.
4. **Para enlaces e imágenes**, añade `attribute`: `href` para un enlace, `src` para una imagen.
5. **Busca el botón de página siguiente** y anota su selector en `next_locator`.
6. Guarda el archivo y pruébalo con pocas páginas:

   ```bash
   bin/adaptive-scraper scrape --recipe mi-receta.json --max-pages 1 --headless=false
   ```

Para comprobar un selector antes de escribirlo, abre la consola del navegador (pestaña **Consola** de las
herramientas de desarrollo) y escribe `document.querySelectorAll("article.product").length`. Debe darte el
número de elementos que ves en la página.

Para una tabla, el elemento repetido es la fila y cada celda se localiza por su posición. El proyecto trae
este ejemplo en `examples/recipes/tabla-demo.json`:

```json
{
  "name": "Lista de precios de demostración",
  "start_url": "http://127.0.0.1:8090/tabla.html",
  "item_locator": "#precios > tbody > tr",
  "fields": [
    {"name": "producto", "type": "text", "locator": "td:nth-of-type(1)", "required": true},
    {"name": "precio", "type": "number", "locator": "td:nth-of-type(2)", "required": true},
    {"name": "disponibilidad", "type": "text", "locator": "td:nth-of-type(3)"}
  ],
  "pagination": {"type": "none"}
}
```

### Reglas que debe cumplir

La aplicación rechaza una receta, con un mensaje que dice qué falla, si:

- Tiene una clave que no existe (por ejemplo, una errata como `"feilds"`).
- Algún selector no es CSS simple. No se admiten XPath ni los prefijos de Playwright como `text=` o `xpath=`.
- Dos campos tienen el mismo nombre, o un nombre lleva espacios o acentos.
- Un selector supera los 500 caracteres o hay más de 50 campos.
- `start_url` no es `http` ni `https`.

### Ejecutar una receta

Desde un archivo, sin guardarla:

```bash
bin/adaptive-scraper scrape --recipe mi-receta.json --output datos.csv
```

Opciones de `scrape` y de `recipes run`:

| Opción | Por defecto | Significado |
|---|---|---|
| `--url` | la de la receta | Empezar en otra página **del mismo sitio**. |
| `--max-pages` | 5 | Páginas como máximo (hasta 1000). |
| `--max-records` | 100 | Registros como máximo (hasta 100000). |
| `--output`, `-o` | pantalla | Archivo de salida. |
| `--format` | según la extensión | `csv` o `json`. Sin `--output` ni `--format`, JSON. |
| `--headless` | `true` | `--headless=false` muestra el navegador. |

Sin `--output`, los datos salen por pantalla y el progreso por el canal de errores, así que puedes
encadenarlo con otros programas:

```bash
bin/adaptive-scraper scrape --recipe mi-receta.json --format csv > datos.csv
```

### Guardar y reutilizar recetas

Guardar una receta te evita llevar el archivo de un lado a otro y la hace visible en la interfaz web.

```bash
bin/adaptive-scraper recipes import mi-receta.json    # la guarda y muestra su identificador
bin/adaptive-scraper recipes list                     # recetas guardadas
bin/adaptive-scraper recipes show <id>                # la muestra en JSON
bin/adaptive-scraper recipes run <id> -o datos.csv    # la ejecuta
```

Ejemplo de `recipes list`:

```text
ID                NOMBRE                     VERSIÓN  CAMPOS  URL
8df82352f395d8ff  Productos de demostración  1        4       http://127.0.0.1:8090/productos.html
```

En la interfaz web, la sección **Recetas** permite además:

| Acción | Qué hace |
|---|---|
| Nueva receta | Abre un editor donde pegar o escribir el JSON. |
| Ejecutar | Te lleva a Nueva extracción con esa receta ya elegida. |
| Editar | Guarda los cambios como una **versión nueva**; las anteriores se conservan. |
| Borrar | Elimina la receta. Los trabajos ya ejecutados conservan la suya. |

Editar y borrar solo están disponibles en la interfaz web y en la API, no en la línea de comandos.

## 6. Extraer con IA, sin escribir la receta

Con la IA activada puedes describir lo que quieres y dejar que un modelo local proponga la receta. Todo
ocurre en tu equipo: no se envía nada a servicios externos.

### Activarla

1. **Instala Ollama** siguiendo las instrucciones de [ollama.com/download](https://ollama.com/download) y
   déjalo en marcha:

   ```bash
   ollama serve
   ```

2. **Activa la IA** en tu archivo `.env`:

   ```text
   AI_ENABLED=true
   OLLAMA_MODEL=llama3.2:3b
   ```

3. **Descarga el modelo** (unos 2 GB para el modelo por defecto). Si la descarga se corta, repite el
   comando y continúa donde quedó:

   ```bash
   make ollama-pull
   ```

4. **Comprueba que todo está listo:**

   ```bash
   bin/adaptive-scraper ai status
   ```

   Debe mostrar `"reachable": true` y `"model_installed": true`. Si no, el campo `message` dice qué falta.

### Elegir el modelo

Puedes usar cualquier modelo de Ollama cambiando `OLLAMA_MODEL`. Un modelo más grande acierta más, pero
necesita más memoria y tarda más, sobre todo sin tarjeta gráfica. Antes de adoptar uno, comprueba que sabe
hacer el trabajo:

```bash
OLLAMA_MODEL=<modelo> make ollama-pull
OLLAMA_MODEL=<modelo> make test-ai
```

`make test-ai` usa el sitio de demostración para descubrir una receta sobre tarjetas, otra sobre una tabla
y reparar una receta tras un rediseño. Si las tres pruebas pasan, el modelo sirve.

### Usarla

Desde la línea de comandos, indica la página y lo que quieres:

```bash
bin/adaptive-scraper scrape \
  --url "http://127.0.0.1:8090/productos.html" \
  --instruction "Extraer nombre, precio y disponibilidad de cada producto" \
  --save-recipe \
  --output productos.csv
```

`--save-recipe` guarda la receta que proponga el modelo y muestra su identificador. A partir de ahí, usa
`recipes run <id>`: es más rápido y no depende del modelo.

En la interfaz web, elige **Describirlo (IA)** en Nueva extracción, escribe la instrucción y la URL. Cuando
el trabajo termine, el botón **Guardar receta** conserva la receta descubierta.

Consejos para la instrucción:

- Nombra los datos uno por uno: «nombre, precio y disponibilidad», no «toda la información».
- Usa la URL de la página que muestra el listado, no la portada del sitio.
- Revisa siempre la vista previa: el modelo puede confundir dos datos parecidos.

### Qué hace la IA y qué no

- **Propone la receta, no navega.** La aplicación analiza la página, numera los datos candidatos y el modelo
  solo indica cuál corresponde a cada cosa que pediste. La receta resultante se valida y se prueba antes de
  usarse.
- **Repara recetas rotas.** Si una receta guardada deja de encontrar elementos, o le falta un campo
  obligatorio en más de la mitad de ellos, el modelo vuelve a localizar los mismos campos. Si la extracción
  termina bien, la receta reparada se guarda como una versión nueva y el trabajo lo avisa.
- **Solo interviene cuando hace falta.** Una receta que funciona se ejecuta sin consultar al modelo.
- **No ve lo que no se repite.** Solo encuentra datos que estén dentro de elementos repetidos al menos tres
  veces en la página. Para casos más raros, escribe la receta a mano.

## 7. Resultados, estados y avisos

### Formatos de salida

| Formato | Contenido |
|---|---|
| CSV | Una fila de cabecera con los nombres de los campos y una fila por registro. Los datos ausentes quedan vacíos. |
| JSON | Una lista de objetos, con los campos en el orden de la receta. Los datos ausentes son `null`. |

### Historial

Cada extracción queda registrada como un **trabajo**, se lance desde donde se lance:

```bash
bin/adaptive-scraper jobs list          # los 20 más recientes
bin/adaptive-scraper jobs show <id>     # detalle en JSON, con todos sus eventos
```

En la interfaz web, la sección **Trabajos** muestra lo mismo. Al abrir uno puedes volver a exportar sus
datos, repetirlo o guardar su receta.

### Estados de un trabajo

| Estado | Significado | Código de salida |
|---|---|---|
| `queued` | Espera su turno (solo en el servidor). | — |
| `running` | En curso. | — |
| `completed` | Terminó. Puede traer avisos. | 0 |
| `failed` | Un error lo interrumpió. Conserva lo extraído hasta ese momento. | 1 |
| `blocked` | El sitio pidió iniciar sesión, mostró un CAPTCHA o denegó el acceso. | 3 |
| `cancelled` | Lo cancelaste (Ctrl+C o el botón Cancelar). Conserva lo extraído. | 4 |

Una receta inválida termina con el código 5, antes de crear ningún trabajo.

### Avisos habituales

| Aviso | Qué significa |
|---|---|
| `campo requerido "x" ausente en N de M registros` | A algunos elementos les falta ese dato. Si son pocos, suele ser normal; si son todos, revisa el selector del campo. |
| `página N: ningún elemento coincide con "..."` | `item_locator` no encuentra nada en esa página. |
| `la paginación no cambió el contenido` | El botón siguiente no llevó a una página nueva; se detuvo para no repetir datos. |
| `N registros duplicados` | Hay registros idénticos. Se conservan; decide tú si sobran. |
| `la receta dejó de funcionar y se reparó; guardada como versión N` | La IA arregló la receta. Conviene revisar la vista previa. |

Los avisos no descartan datos: los registros se guardan igualmente.

## 8. Problemas frecuentes

| Síntoma | Causa probable y solución |
|---|---|
| `iniciar Playwright (¿falta make install-browsers?)` | No se descargó el navegador. Ejecuta `make install-browsers`. |
| Termina con 0 registros | `item_locator` no coincide con nada. Compruébalo en la consola del navegador y ejecuta con `--headless=false` para ver la página. |
| Todos los valores de un campo salen vacíos | El `locator` del campo es incorrecto, o el dato está en un atributo y falta `attribute`. |
| Un precio sale vacío | El campo es `number` y el texto no contiene ningún número. Usa `text` para ver qué se lee. |
| Solo lee la primera página | Falta `pagination`, o `next_locator` no coincide con el botón. |
| `el host "..." no está permitido por la receta` | `--url` apunta a otro sitio. Añádelo a `allowed_hosts` o crea otra receta. |
| `la navegación salió de los hosts permitidos` | El sitio redirige a otro dominio (por ejemplo, a `www.`). Añade ese dominio a `allowed_hosts`. |
| `acceso bloqueado` | El sitio exige iniciar sesión o verificación humana. La aplicación no lo sortea. |
| La web dice que la IA no está disponible | Ejecuta `bin/adaptive-scraper ai status` y sigue lo que indique `message`. |
| `falta la receta; para descubrirla... active la IA` | Usaste `--instruction` con la IA desactivada. Pon `AI_ENABLED=true` en `.env`. |
| No aparecen las recetas que guardaste | Ejecutas el programa desde otra carpeta. Usa una ruta absoluta en `APP_DATA_DIR`. |
| El servidor avisa `interfaz web no disponible` | Falta compilar la interfaz: `make web-build`. |

## 9. Límites y uso responsable

- La aplicación **no** resuelve CAPTCHA, no evade sistemas anti-bot y no inicia sesión. Si un sitio lo
  exige, el trabajo termina como `blocked`.
- Eres responsable de respetar las condiciones de uso de cada sitio, su `robots.txt`, los derechos sobre
  los datos y los límites de acceso. Empieza siempre con pocas páginas.
- Solo extrae listados: elementos repetidos en una página, con paginación por botón. No sigue los enlaces
  de cada elemento a su página de detalle, no hace scroll infinito y no lee contenido dentro de marcos.
- El servidor no tiene contraseña y todavía no filtra las direcciones a las que navega. Úsalo solo en tu
  equipo y solo con recetas tuyas o de confianza.
