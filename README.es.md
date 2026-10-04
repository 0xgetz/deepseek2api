<div align="center">
  <img src="assets/logo.png" alt="deepseek2api" width="180" />

  <h1>deepseek2api</h1>

  <p><strong>Convierte <a href="https://chat.deepseek.com">chat.deepseek.com</a> en una API compatible con OpenAI.</strong><br/>
  Un proxy Go ligero y sin dependencias que habla el protocolo OpenAI Chat Completions.</p>

  <p>
    <a href="https://github.com/0xgetz/deepseek2api/stargazers"><img alt="GitHub stars" src="https://img.shields.io/github/stars/0xgetz/deepseek2api?style=for-the-badge&logo=github&color=7c5cff"></a>
    <a href="https://github.com/0xgetz/deepseek2api/network/members"><img alt="GitHub forks" src="https://img.shields.io/github/forks/0xgetz/deepseek2api?style=for-the-badge&logo=github&color=26e0c8"></a>
    <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/github/license/0xgetz/deepseek2api?style=for-the-badge&color=4f8bff"></a>
  </p>
  <p>
    <img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/0xgetz/deepseek2api?style=for-the-badge&logo=go&color=00add8">
    <img alt="Zero dependencies" src="https://img.shields.io/badge/dependencies-0-brightgreen?style=for-the-badge">
    <img alt="Docker ready" src="https://img.shields.io/badge/docker-ready-2496ed?style=for-the-badge&logo=docker&logoColor=white">
  </p>
  <p>
    <a href="README.md"><img alt="English" src="https://img.shields.io/badge/lang-English-4f8bff?style=flat-square"></a>
    <a href="README.id.md"><img alt="Bahasa Indonesia" src="https://img.shields.io/badge/lang-Indonesia-26e0c8?style=flat-square"></a>
    <a href="README.zh.md"><img alt="中文" src="https://img.shields.io/badge/lang-中文-ff6b6b?style=flat-square"></a>
    <a href="README.ja.md"><img alt="日本語" src="https://img.shields.io/badge/lang-日本語-ffb454?style=flat-square"></a>
    <a href="README.es.md"><img alt="Español" src="https://img.shields.io/badge/lang-Español-7c5cff?style=flat-square"></a>
  </p>
</div>

---

## ¿Qué es esto?

`deepseek2api` envuelve la **aplicación web chat.deepseek.com** detrás de una
API HTTP limpia y compatible con OpenAI. Está escrito en Go puro usando solo la
biblioteca estándar, por lo que se compila como un único binario estático sin
dependencias en tiempo de ejecución.

Apunta cualquier cliente OpenAI — Cherry Studio, LobeChat, Open WebUI, los SDK
oficiales de OpenAI, `curl` — y habla con DeepSeek sin una API key oficial.

> **Trae tu propia cuenta.** Tú proporcionas el `userToken` web de tu propia
> sesión de navegador iniciada. El proxy no envía datos a terceros.

## Características

- **Compatible con OpenAI Chat Completions** — `POST /v1/chat/completions`, con y sin streaming.
- **Lista de modelos** — `GET /v1/models`.
- **Entrada de imágenes** — los bloques `image_url` aceptan data URL base64 y enlaces http(s); las imágenes se suben a DeepSeek y se envían como `ref_file_ids`.
- **Proof-of-work automático** — el servidor resuelve el reto DeepSeekHashV1 (variante de Keccak de 23 rondas) en paralelo, normalmente en menos de 100 ms.
- **Soporte de razonamiento** — la salida de pensamiento se mapea a `reasoning_content`, igual que la API oficial de DeepSeek.
- **Grupo de tokens multiusuario** — round-robin entre cuentas; los tokens permanecen en el servidor.
- **Contexto multiturno** — la caché automática de prefijos reutiliza una sesión de DeepSeek; también admite el paso directo de `conversation_id`.
- **Nombres de modelo a prueba de futuro** — los id `deepseek-*` desconocidos se mapean heurísticamente (palabras clave reasoner/think/search).
- **Auto-limpiante** — las sesiones inactivas se eliminan en el upstream para mantener limpia tu lista de chats.
- **Listo para Docker / Docker Compose.**

## Modelos compatibles

| ID de modelo | Comportamiento web | Alias |
| --- | --- | --- |
| `deepseek-chat` | Modo rápido | `deepseek-v3` |
| `deepseek-reasoner` | Modo rápido + pensamiento profundo | `deepseek-r1` |
| `deepseek-search` | Modo rápido + búsqueda web | |
| `deepseek-reasoner-search` | Pensamiento profundo + búsqueda web | |

## Obtener un token

1. Inicia sesión en [chat.deepseek.com](https://chat.deepseek.com) en tu navegador.
2. Abre DevTools (F12) → Console y ejecuta:

   ```js
   JSON.parse(localStorage.userToken).value
   ```

3. Copia la salida. Permanece válida hasta que cierres sesión; repite para más cuentas.

## Inicio rápido

### Compilar

```bash
go build -o deepseek2api .
```

### Ejecutar

Una cuenta:

```bash
PROXY_API_KEY='tu-clave-proxy' DEEPSEEK_TOKEN='tu-token' PORT=8080 ./deepseek2api
```

Varias cuentas: crea `accounts.txt` en el directorio de trabajo, un token por
línea (las líneas vacías y los comentarios `#` se ignoran):

```text
eyJhbGciOi...token1
eyJhbGciOi...token2
```

```bash
PROXY_API_KEY='tu-clave-proxy' ./deepseek2api
```

### Probar

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer tu-clave-proxy' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "deepseek-reasoner",
    "messages": [{"role": "user", "content": "Hola"}],
    "stream": true
  }'
```

## Docker

```bash
docker build -t deepseek2api .

docker run --rm -p 8080:8080 \
  -e PROXY_API_KEY='tu-clave-proxy' \
  -e DEEPSEEK_TOKEN='tu-token' \
  deepseek2api
```

O con Compose:

```bash
PROXY_API_KEY='tu-clave-proxy' DEEPSEEK_TOKEN='tu-token' docker compose up --build
```

## Configuración

| Variable | Predeterminado | Descripción |
| --- | --- | --- |
| `PORT` | `8080` | Puerto HTTP local |
| `PROXY_API_KEY` | ninguno — obligatorio | Clave que usan los clientes para llamar al proxy |
| `DEEPSEEK_TOKEN` | vacío | `userToken` web de DeepSeek |
| `DEEPSEEK_ACCOUNTS_FILE` | `accounts.txt` | Archivo multicuenta, un token por línea |
| `DEEPSEEK_BASE_URL` | `https://chat.deepseek.com` | URL base del upstream |
| `DEFAULT_MODEL` | `deepseek-chat` | Modelo cuando la petición no especifica uno |
| `CONVERSATION_TTL` | `30m` | Tiempo inactivo antes de borrar la sesión en el upstream |
| `MAX_CONVERSATIONS` | `1024` | Máximo de sesiones en memoria (evicción LRU) |

## Conversaciones multiturno

- **Modo automático (recomendado).** Los clientes envían el array `messages`
  completo como siempre. El proxy huella el historial (todo excepto el último
  mensaje); si acierta, reutiliza la misma sesión de DeepSeek y envía solo el
  turno nuevo; si no, crea una sesión nueva. Los clientes populares funcionan
  sin cambios.
- **Modo de paso directo.** Las respuestas incluyen un campo no estándar
  `conversation_id` (el id de sesión de DeepSeek). Envíalo en el cuerpo de una
  petición posterior para reutilizar esa sesión; solo el último mensaje de
  usuario se usa como prompt.

> Nota de arranque en frío: tras reiniciar, si el historial tiene más de un
> turno, solo se reenvían el prompt de sistema y el último mensaje del usuario.

## Autenticación

Cada petición a `/v1/*` requiere:

```http
Authorization: Bearer <PROXY_API_KEY>
```

`DEEPSEEK_TOKEN` / `accounts.txt` se usan solo en el servidor y nunca se
exponen a quien llama.

## Notas y límites

- **Imágenes:** ≤ 20 MB cada una, hasta 8 por petición, png/jpg/webp/gif/bmp.
  Solo se procesan las imágenes del último mensaje de usuario.
- **`usage` es estimado:** el total proviene del delta acumulado de tokens de la
  sesión del upstream, y el desglose se aproxima por número de caracteres.
- El `429` del upstream se mapea a `429`; un token inválido se mapea a `401`.
- **No** subas tus tokens a un repositorio público (`accounts.txt` ya está en `.gitignore`).

## Estructura del proyecto

```
main.go                 servidor HTTP y enrutamiento
config/                 configuración por entorno
handlers/               manejadores compatibles con OpenAI, SSE, subida de imágenes
deepseek/               cliente del upstream, proof-of-work, streaming
docs/api-analysis.md    notas de ingeniería inversa de la API del upstream
assets/                 logo y banner
```

## Licencia

Publicado bajo la [Licencia MIT](LICENSE).

<div align="center"><sub>Sin afiliación con DeepSeek. Úsalo con responsabilidad y respeta los términos del upstream.</sub></div>
