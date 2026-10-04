<div align="center">
  <img src="assets/logo.png" alt="deepseek2api" width="180" />

  <h1>deepseek2api</h1>

  <p><strong><a href="https://chat.deepseek.com">chat.deepseek.com</a> を OpenAI 互換 API に。</strong><br/>
  OpenAI Chat Completions プロトコルを話す、軽量・依存ゼロの Go プロキシ。</p>

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

## これは何？

`deepseek2api` は **chat.deepseek.com のウェブ版**を、クリーンで OpenAI 互換の
HTTP API としてラップします。純粋な Go 標準ライブラリのみで書かれており、
ランタイム依存のない単一の静的バイナリとして動作します。

任意の OpenAI クライアント（Cherry Studio、LobeChat、Open WebUI、公式 OpenAI
SDK、`curl`）を向けるだけで、公式 API キーなしに DeepSeek を利用できます。

> **アカウントはご自身で。** ログイン済みブラウザセッションのウェブ `userToken`
> を指定します。プロキシは外部にデータを送信しません。

## 特徴

- **OpenAI Chat Completions 互換** — `POST /v1/chat/completions`、ストリーミング／非ストリーミング対応。
- **モデル一覧** — `GET /v1/models`。
- **画像入力** — `image_url` ブロックは base64 data URL と http(s) リンクに対応。画像は DeepSeek にアップロードし `ref_file_ids` として送信。
- **自動 proof-of-work** — DeepSeekHashV1（23 ラウンドの Keccak 変種）を並列で解き、通常 100ms 未満。
- **推論サポート** — thinking 出力を `reasoning_content` にマッピング（公式 API と同一）。
- **マルチアカウントのトークンプール** — ラウンドロビン。トークンはサーバー側のみ。
- **マルチターン文脈** — 自動プレフィックスキャッシュでセッションを再利用。`conversation_id` の明示的なパススルーも可能。
- **将来のモデル名に対応** — 未知の `deepseek-*` はヒューリスティックにマッピング（reasoner/think/search キーワード）。
- **自動クリーンアップ** — アイドルセッションは上流から削除され、ウェブのチャット一覧を汚しません。
- **Docker / Docker Compose 対応。**

## 対応モデル

| モデル ID | ウェブでの挙動 | 別名 |
| --- | --- | --- |
| `deepseek-chat` | 高速モード | `deepseek-v3` |
| `deepseek-reasoner` | 高速モード + 深い思考 | `deepseek-r1` |
| `deepseek-search` | 高速モード + ウェブ検索 | |
| `deepseek-reasoner-search` | 深い思考 + ウェブ検索 | |

## トークンの取得

1. ブラウザで [chat.deepseek.com](https://chat.deepseek.com) にログイン。
2. DevTools（F12）→ Console で実行：

   ```js
   JSON.parse(localStorage.userToken).value
   ```

3. 出力をコピー。ログアウトするまで有効です。追加アカウントは繰り返します。

## クイックスタート

### ビルド

```bash
go build -o deepseek2api .
```

### 実行

単一アカウント：

```bash
PROXY_API_KEY='あなたのプロキシキー' DEEPSEEK_TOKEN='あなたのトークン' PORT=8080 ./deepseek2api
```

複数アカウント：作業ディレクトリに `accounts.txt` を作成し、1 行 1 トークン
（空行と `#` コメントは無視）：

```text
eyJhbGciOi...token1
eyJhbGciOi...token2
```

```bash
PROXY_API_KEY='あなたのプロキシキー' ./deepseek2api
```

### テスト

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer あなたのプロキシキー' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "deepseek-reasoner",
    "messages": [{"role": "user", "content": "こんにちは"}],
    "stream": true
  }'
```

## Docker

```bash
docker build -t deepseek2api .

docker run --rm -p 8080:8080 \
  -e PROXY_API_KEY='あなたのプロキシキー' \
  -e DEEPSEEK_TOKEN='あなたのトークン' \
  deepseek2api
```

または Compose：

```bash
PROXY_API_KEY='あなたのプロキシキー' DEEPSEEK_TOKEN='あなたのトークン' docker compose up --build
```

## 設定

| 変数 | 既定値 | 説明 |
| --- | --- | --- |
| `PORT` | `8080` | ローカル HTTP ポート |
| `PROXY_API_KEY` | なし — 必須 | クライアントがプロキシを呼ぶ鍵 |
| `DEEPSEEK_TOKEN` | 空 | DeepSeek ウェブの `userToken` |
| `DEEPSEEK_ACCOUNTS_FILE` | `accounts.txt` | マルチアカウントファイル（1 行 1 トークン） |
| `DEEPSEEK_BASE_URL` | `https://chat.deepseek.com` | 上流ベース URL |
| `DEFAULT_MODEL` | `deepseek-chat` | リクエストで未指定時のモデル |
| `CONVERSATION_TTL` | `30m` | セッションを上流から削除するまでのアイドル時間 |
| `MAX_CONVERSATIONS` | `1024` | メモリ保持する最大セッション数（LRU 淘汰） |

## マルチターン会話

- **自動モード（推奨）**：クライアントは通常どおり完全な `messages` を送信。
  プロキシは履歴を指紋化し、ヒット時は同じセッションを再利用して最新の 1 往復
  のみ送信、ミス時は新規セッションを作成します。主要クライアントは変更不要。
- **パススルーモード**：レスポンスに非標準の `conversation_id`（DeepSeek セッ
  ション ID）が含まれます。次のリクエスト本文に含めるとそのセッションを再利用
  します（最後の user メッセージのみをプロンプトに使用）。

> コールドスタート：再起動後、履歴が 1 往復を超える場合、system プロンプトと
> 最新のユーザーメッセージのみが再送されます。

## 認証

すべての `/v1/*` リクエストに必要：

```http
Authorization: Bearer <PROXY_API_KEY>
```

`DEEPSEEK_TOKEN` / `accounts.txt` はサーバー側でのみ使用され、呼び出し元には
公開されません。

## 注意事項

- **画像**：各 ≤20MB、1 リクエスト最大 8 枚、png/jpg/webp/gif/bmp。最後の user
  メッセージの画像のみ処理されます。
- **`usage` は推定値**：合計は上流セッションの累積トークン差分、内訳は文字数で近似。
- 上流の `429` は `429` に、無効トークンは `401` にマッピング。
- トークンを公開リポジトリにコミットしないでください（`accounts.txt` は `.gitignore` 済み）。

## プロジェクト構成

```
main.go                 HTTP サーバーとルーティング
config/                 環境設定
handlers/               OpenAI 互換ハンドラ、SSE、画像アップロード
deepseek/               上流クライアント、proof-of-work、ストリーミング
docs/api-analysis.md    上流 API のリバースエンジニアリング記録
assets/                 ロゴとバナー
```

## ライセンス

[MIT ライセンス](LICENSE) の下で公開されています。

<div align="center"><sub>DeepSeek とは無関係です。責任を持って利用し、上流の規約を尊重してください。</sub></div>
