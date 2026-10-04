<div align="center">
  <img src="assets/logo.png" alt="deepseek2api" width="180" />

  <h1>deepseek2api</h1>

  <p><strong>Turn <a href="https://chat.deepseek.com">chat.deepseek.com</a> into an OpenAI-compatible API.</strong><br/>
  A lightweight, dependency-free Go proxy that speaks the OpenAI Chat Completions protocol.</p>

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

## What is this?

`deepseek2api` wraps the **chat.deepseek.com web app** behind a clean,
OpenAI-compatible HTTP API. It is written in pure Go using only the standard
library, so it ships as a single static binary with no runtime dependencies.

Point any OpenAI client at it — Cherry Studio, LobeChat, Open WebUI, the
official OpenAI SDKs, `curl` — and talk to DeepSeek without an official API key.

> **Bring your own account.** You supply the web `userToken` from your own
> logged-in browser session. The proxy never phones home.

## Features

- **OpenAI Chat Completions compatible** — `POST /v1/chat/completions`, streaming and non-streaming.
- **Model listing** — `GET /v1/models`.
- **Image input** — `image_url` content blocks accept base64 data URLs and http(s) links; images are uploaded to DeepSeek and sent as `ref_file_ids`.
- **Automatic proof-of-work** — the server solves the web client's DeepSeekHashV1 challenge (a 23-round Keccak variant) in parallel, typically under 100 ms.
- **Reasoning support** — thinking output is mapped to `reasoning_content`, matching the official DeepSeek API.
- **Multi-account token pool** — round-robin across accounts; tokens stay server-side.
- **Multi-turn context** — automatic prefix caching reuses a DeepSeek session; an explicit `conversation_id` passthrough is also supported.
- **Future-proof model names** — unknown `deepseek-*` ids are mapped heuristically (reasoner/think/search keywords), so clients that hardcode newer names keep working.
- **Self-cleaning** — idle sessions are deleted upstream so your web chat list stays tidy.
- **Docker / Docker Compose** ready.

## Supported models

| Model ID | Web behavior | Aliases |
| --- | --- | --- |
| `deepseek-chat` | Fast mode | `deepseek-v3` |
| `deepseek-reasoner` | Fast mode + deep thinking | `deepseek-r1` |
| `deepseek-search` | Fast mode + web search | |
| `deepseek-reasoner-search` | Deep thinking + web search | |

## Getting a token

1. Log in to [chat.deepseek.com](https://chat.deepseek.com) in your browser.
2. Open DevTools (F12) → Console and run:

   ```js
   JSON.parse(localStorage.userToken).value
   ```

3. Copy the output. It stays valid until you sign out; repeat for extra accounts.

## Quick start

### Build

```bash
go build -o deepseek2api .
```

### Run

Single account:

```bash
PROXY_API_KEY='your-proxy-key' DEEPSEEK_TOKEN='your-token' PORT=8080 ./deepseek2api
```

Multiple accounts: create `accounts.txt` in the working directory, one token per
line (blank lines and `#` comments are ignored):

```text
eyJhbGciOi...token1
eyJhbGciOi...token2
```

```bash
PROXY_API_KEY='your-proxy-key' ./deepseek2api
```

### Test

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer your-proxy-key' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "deepseek-reasoner",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
  }'
```

## Docker

```bash
docker build -t deepseek2api .

docker run --rm -p 8080:8080 \
  -e PROXY_API_KEY='your-proxy-key' \
  -e DEEPSEEK_TOKEN='your-token' \
  deepseek2api
```

Or with Compose:

```bash
PROXY_API_KEY='your-proxy-key' DEEPSEEK_TOKEN='your-token' docker compose up --build
```

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8080` | Local HTTP port |
| `PROXY_API_KEY` | none — required | Key clients use to call the proxy |
| `DEEPSEEK_TOKEN` | empty | DeepSeek web `userToken` |
| `DEEPSEEK_ACCOUNTS_FILE` | `accounts.txt` | Multi-account file, one token per line |
| `DEEPSEEK_BASE_URL` | `https://chat.deepseek.com` | Upstream base URL |
| `DEFAULT_MODEL` | `deepseek-chat` | Model used when a request omits one |
| `CONVERSATION_TTL` | `30m` | Idle time before a session is deleted upstream |
| `MAX_CONVERSATIONS` | `1024` | Max sessions kept in memory (LRU eviction) |

## Multi-turn conversations

- **Automatic mode (recommended).** Clients send the full `messages` array as
  usual. The proxy fingerprints the history (everything except the last
  message); on a hit it reuses the same DeepSeek session and sends only the new
  turn, otherwise it creates a fresh session. Mainstream clients work with no
  changes.
- **Passthrough mode.** Responses include a non-standard `conversation_id`
  field (the DeepSeek session id). Send it in a later request body to reuse that
  session; only the last user message is used as the prompt.

> Cold start note: after a restart, if the history has more than one turn, only
> the system prompt and the latest user message are replayed. This is a
> deliberate simplification.

## Authentication

Every `/v1/*` request needs:

```http
Authorization: Bearer <PROXY_API_KEY>
```

`DEEPSEEK_TOKEN` / `accounts.txt` are used server-side only and are never
exposed to callers.

## Notes & limits

- **Images:** ≤ 20 MB each, up to 8 per request, png/jpg/webp/gif/bmp. Only the
  last user message's images are processed; older ones already live in the
  upstream session. Vision requests add a few seconds of first-token latency.
- **Usage** is estimated: totals come from the upstream session's cumulative
  token delta, and the split is approximated by character count.
- Upstream `429` maps to `429`; an invalid token maps to `401`.
- Do **not** commit your tokens. `accounts.txt` is already in `.gitignore`.

## Project layout

```
main.go                 HTTP server and routing
config/                 environment configuration
handlers/               OpenAI-compatible handlers, SSE, image upload
deepseek/               upstream client, proof-of-work, streaming
docs/api-analysis.md    reverse-engineering notes on the upstream API
assets/                 logo and banner
```

## License

Released under the [MIT License](LICENSE).

<div align="center"><sub>Not affiliated with DeepSeek. Use responsibly and respect upstream terms.</sub></div>
