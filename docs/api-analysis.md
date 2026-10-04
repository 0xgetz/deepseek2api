# chat.deepseek.com 网页端 API 逆向分析

> 2026-10-03 通过浏览器内 hook（fetch/XHR 包装）抓包实测得出，所有请求均经真实账号验证。
> POW 算法已用真实样本精确复现（哈希与 challenge 完全一致）。

## 1. 认证

- 登录后 token 存在 `localStorage.userToken`：`{"value":"<token>","__version":"0"}`。
- 所有 `/api/v0/*` 请求只用 `Authorization: Bearer <token>`，无需 Cookie。
- `x-device-id` 是客户端自生成的 UUIDv4，存于 `localStorage["deepseek-device-id:chat"]`，服务端接受任意 UUID。

## 2. 通用请求头

```http
Authorization: Bearer <userToken>
Content-Type: application/json
x-client-bundle-id: com.deepseek.chat
x-client-locale: zh_CN
x-client-platform: web
x-client-timezone-offset: 28800
x-client-version: 2.5.0
x-device-id: <uuid-v4>
x-device-model:            （空字符串）
```

> 网关提示：缺 `user-agent` / `referer` / `origin` 等浏览器特征头时，非浏览器请求会被网关
> 软拦截（返回前端 HTML 兜底页或 429）。服务端复现时必须带全这些头。

## 3. 补全流程（三步）

### 3.1 取 POW 挑战

```http
POST /api/v0/chat/create_pow_challenge
{"target_path":"/api/v0/chat/completion"}
```

响应：

```json
{"code":0,"msg":"","data":{"biz_code":0,"biz_msg":"","biz_data":{"challenge":{
  "algorithm":"DeepSeekHashV1",
  "challenge":"168efa309e5a73033fc82dde4333bbc6515da440ef961574f73f99cbe7b397ad",
  "salt":"a40318e6b7af56bf27cf",
  "signature":"68abc6d6d301b6d89a4075622ace2c909c6262860e813230bc519f78cd0aa159",
  "difficulty":144000,
  "expire_at":1790959021012,
  "expire_after":300000,
  "target_path":"/api/v0/chat/completion"
}}}}
```

### 3.2 求解 POW（DeepSeekHashV1，已精确验证）

- 消息：`"{salt}_{expire_at}_{nonce}"`（十进制 nonce，从 0 递增）
- 哈希：**Keccak-f[1600] 变体** —— rate=136、SHA3 填充（0x06 … 0x80）、**只做 23 轮**
  （轮常数用标准 RC[1..23]，**跳过 RC[0]=0x01**；rho/pi/chi/theta 与标准一致）
- 输出：state 前 4 个 lane（小端）共 32 字节，hex 后与 `challenge` 相等即为解。
- 求解：`nonce ∈ [0, ∞)` 暴力递增，期望约 `difficulty` 次哈希（实测 144000 难度 ≈ 58595~107088 次）。

### 3.3 发送补全

```http
POST /api/v0/chat/completion
x-ds-pow-response: base64(JSON{
  "algorithm":"DeepSeekHashV1",
  "challenge":"<challenge>",
  "salt":"<salt>",
  "answer":<nonce>,
  "signature":"<signature>",
  "target_path":"/api/v0/chat/completion"
})

{"chat_session_id":"<sid>","parent_message_id":null,"model_type":"default",
 "prompt":"<用户消息>","ref_file_ids":[],"thinking_enabled":true,
 "search_enabled":true,"action":null,"preempt":false}
```

- 首条消息 `model_type:"default"`；同会话后续轮次传 `null`。
- `parent_message_id`：首条 `null`；后续 = 上一轮 `ready` 事件中的 `response_message_id`。
- 会话内多轮上下文由服务端维护，客户端只需发最新一条。

## 4. 会话与文件

- 建会话：`POST /api/v0/chat_session/create`，body `{}` → `data.biz_data.chat_session.id`
- 删会话：`POST /api/v0/chat_session/delete`，body `{"chat_session_id":"<sid>"}`

### 4.1 图片/文件上传（识图链路）

1. 取 POW：`create_pow_challenge`，`target_path = "/api/v0/file/upload_file"`（上传有独立 POW）
2. 上传：`POST /api/v0/file/upload_file`，multipart/form-data，单个字段 `file`
   （带文件名扩展名），带 `x-ds-pow-response` 头 → `biz_data = {id, status, file_name, file_size, ...}`
3. 轮询解析：`GET /api/v0/file/fetch_files?file_ids=<id,id,...>` → `biz_data.files[].status`，
   状态机 `PENDING → PARSING → SUCCESS / FAILED / CONTENT_FILTER / CONTENT_TOO_LONG / CANCELLED`
4. 补全时把文件 id 放进 `ref_file_ids` 数组即可，`model_type` 仍为 `default`（识图已并入主模型）

## 5. 模型

`GET /api/v0/client/settings?did=<uuid>&scope=model` 返回 `model_configs`：

| model_type | 名称 | 说明 |
| --- | --- | --- |
| `default` | 快速模式 | 默认；思考/搜索是独立开关 |
| `expert` | 专家模式 | 当前未开放（enabled=false） |
| `vision` | 识图模式 | 当前未开放 |

补全请求中 `thinking_enabled` / `search_enabled` 控制深度思考与联网搜索。

## 6. 补全响应（text/event-stream）

帧格式 `event: <name>\ndata: <json>\n\n`，实测帧序列：

```
event: ready
data: {"request_message_id":1,"response_message_id":2,"model_type":"default"}

event: update_session
data: {"updated_at":...}

data: {"v":{"response":{..., "fragments":[{"id":2,"type":"THINK","content":"我们需要",...}]}}}

data: {"p":"response/fragments/-1/content","o":"APPEND","v":"回答"}
data: {"v":"用户"}          ← 裸 v = 继续上一次 patch（追加到 fragments/-1/content）
...
data: {"p":"response/fragments/-1/elapsed_secs","o":"SET","v":0.615670173}

data: {"p":"response/fragments","o":"APPEND","v":[{"id":3,"type":"RESPONSE","content":"1",...}]}
data: {"p":"response/fragments/-1/content","v":"+"}   ← 缺 o 亦为追加
data: {"v":"1"} ...
...
data: {"p":"response","o":"BATCH","v":[{"p":"accumulated_token_usage","v":74},{"p":"quasi_status","v":"FINISHED"}]}
data: {"p":"response/status","o":"SET","v":"FINISHED"}

event: title
data: {"content":"1加1等于2"}

event: close
data: {"click_behavior":"none","auto_resume":false}
```

解析要点：

- fragment `type`：`THINK` → 思考内容（→ OpenAI `reasoning_content`）；`RESPONSE` → 正文（→ `content`）。
- 初始 `{"v":{object}}` 内含 fragments 数组，其中已有第一段 THINK 文本，必须提取。
- 裸 `{"v":"..."}`：v 为字符串时按上一条 patch 的路径追加；`p` 缺省沿用上一条；`-1` 指最后一个 fragment。
- `accumulated_token_usage` 是**会话累计值**，单轮用量 = 本轮值 − 上一轮值。
- `event: close` 或 `status=FINISHED` 即结束。

## 7. 其他观测

- 每次会话自动生成标题（`event: title`）。
- `x-client-timezone-offset` 单位为**秒**（UTC+8 = 28800）。
- 登录/注册另有 guest POW（`/api/v0/users/create_guest_challenge`，响应头格式只有 salt+answer），代理场景用不到。
- API 端点无 Cookie 校验、未发现 TLS 指纹拦截（Go 标准库 HTTP 客户端 + 浏览器请求头即可直连，实测通过）。
- **账号级限流**：短时间密集的会话操作（如连续删除）会触发 429，间隔 1.5s 逐个操作即可；
  补全请求正常频率下未触发。429 响应体为 JSON envelope。
