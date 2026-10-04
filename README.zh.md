<div align="center">
  <img src="assets/logo.png" alt="deepseek2api" width="180" />

  <h1>deepseek2api</h1>

  <p><strong>把 <a href="https://chat.deepseek.com">chat.deepseek.com</a> 变成 OpenAI 兼容 API。</strong><br/>
  一个轻量、零依赖的 Go 代理，完整实现 OpenAI Chat Completions 协议。</p>

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

## 这是什么？

`deepseek2api` 把 **chat.deepseek.com 网页版**包装成干净、OpenAI 兼容的
HTTP API。使用纯 Go 标准库编写，编译为单个静态二进制文件，无任何运行时依赖。

把任意 OpenAI 客户端指向它 —— Cherry Studio、LobeChat、Open WebUI、官方
OpenAI SDK、`curl` —— 即可在没有官方 API Key 的情况下使用 DeepSeek。

> **自带账号。** 你提供自己已登录浏览器会话中的网页版 `userToken`。代理不会
> 向任何第三方发送数据。

## 功能特性

- **OpenAI Chat Completions 兼容** —— `POST /v1/chat/completions`，支持流式与非流式。
- **模型列表** —— `GET /v1/models`。
- **图片输入** —— `image_url` 内容块支持 base64 data URL 与 http(s) 链接，自动上传为 DeepSeek 文件并以 `ref_file_ids` 发送。
- **服务端自动完成 proof-of-work** —— 并行求解 DeepSeekHashV1（23 轮 Keccak 变体），通常 <100ms。
- **推理支持** —— 思考过程映射为 `reasoning_content`，与官方 API 一致。
- **多账号令牌池** —— 轮询分发，令牌仅保存在服务端。
- **多轮对话上下文** —— 自动前缀缓存复用 DeepSeek 会话，也支持显式 `conversation_id` 直通。
- **面向未来的模型名** —— 未知 `deepseek-*` 名称按启发式映射（reasoner/think/search 关键词）。
- **自动清理** —— 空闲会话自动从上游删除，保持网页版会话列表整洁。
- **Docker / Docker Compose 部署。**

## 支持的模型

| 模型 ID | 网页端行为 | 别名 |
| --- | --- | --- |
| `deepseek-chat` | 快速模式 | `deepseek-v3` |
| `deepseek-reasoner` | 快速模式 + 深度思考 | `deepseek-r1` |
| `deepseek-search` | 快速模式 + 联网搜索 | |
| `deepseek-reasoner-search` | 深度思考 + 联网搜索 | |

## 获取 Token

1. 浏览器登录 [chat.deepseek.com](https://chat.deepseek.com)；
2. F12 打开控制台，执行：

   ```js
   JSON.parse(localStorage.userToken).value
   ```

3. 复制输出的 token（退出登录前一直有效；换号重复即可）。

## 快速开始

### 构建

```bash
go build -o deepseek2api .
```

### 运行

单账号：

```bash
PROXY_API_KEY='你的代理密钥' DEEPSEEK_TOKEN='你的token' PORT=8080 ./deepseek2api
```

多账号：在工作目录创建 `accounts.txt`，每行一个 token（`#` 与空行忽略）：

```text
eyJhbGciOi...token1
eyJhbGciOi...token2
```

```bash
PROXY_API_KEY='你的代理密钥' ./deepseek2api
```

### 测试

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer 你的代理密钥' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "deepseek-reasoner",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": true
  }'
```

## Docker

```bash
docker build -t deepseek2api .

docker run --rm -p 8080:8080 \
  -e PROXY_API_KEY='你的代理密钥' \
  -e DEEPSEEK_TOKEN='你的token' \
  deepseek2api
```

或使用 Compose：

```bash
PROXY_API_KEY='你的代理密钥' DEEPSEEK_TOKEN='你的token' docker compose up --build
```

## 配置项

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8080` | 本地 HTTP 服务端口 |
| `PROXY_API_KEY` | 无，必填 | 客户端调用代理用的密钥 |
| `DEEPSEEK_TOKEN` | 空 | DeepSeek 网页版 `userToken` |
| `DEEPSEEK_ACCOUNTS_FILE` | `accounts.txt` | 多账号文件，每行一个 token |
| `DEEPSEEK_BASE_URL` | `https://chat.deepseek.com` | 上游地址 |
| `DEFAULT_MODEL` | `deepseek-chat` | 请求未指定模型时使用 |
| `CONVERSATION_TTL` | `30m` | 会话空闲多久后从上游删除 |
| `MAX_CONVERSATIONS` | `1024` | 内存中保留的最大会话数（LRU 淘汰） |

## 多轮对话

- **自动模式（推荐）**：客户端照常发送完整 `messages`，代理对历史做指纹缓存——命中时复用同一会话只发最新一句，未命中时新建会话。主流客户端无需改动。
- **直通模式**：响应带非标准字段 `conversation_id`（DeepSeek 会话 ID）；后续请求带上它即可复用该会话（仅取最后一条 user 消息作为 prompt）。

> 冷启动说明：服务重启后，若历史超过一轮，只会带上 system 提示词与最后一条用户消息。

## 认证方式

所有 `/v1/*` 请求需要：

```http
Authorization: Bearer <PROXY_API_KEY>
```

`DEEPSEEK_TOKEN` / `accounts.txt` 只在服务端使用，不会暴露给调用方。

## 注意事项

- **图片**：每张 ≤20MB，单次最多 8 张，支持 png/jpg/webp/gif/bmp；仅处理最后一条用户消息中的图片。
- **`usage` 是估算值**：总量来自上游会话累计 token 的轮间差，分项按字符数近似。
- 上游 `429` 原样映射为 `429`；令牌失效映射为 `401`。
- 请勿将 token 提交到公开仓库（`accounts.txt` 已在 `.gitignore` 中）。

## 项目结构

```
main.go                 HTTP 服务与路由
config/                 环境配置
handlers/               OpenAI 兼容处理器、SSE、图片上传
deepseek/               上游客户端、proof-of-work、流式处理
docs/api-analysis.md    上游 API 逆向分析笔记
assets/                 Logo 与横幅
```

## 许可证

基于 [MIT 许可证](LICENSE) 发布。

<div align="center"><sub>与 DeepSeek 无隶属关系。请合理使用并遵守上游条款。</sub></div>
