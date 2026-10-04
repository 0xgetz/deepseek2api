# Contributing to deepseek2api

Thanks for taking the time to improve **deepseek2api**. This project is a small,
dependency-free Go proxy, so the contribution bar is deliberately low — clear
code beats clever code.

## Ground rules

- Be respectful. See [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).
- Keep the **zero third-party dependency** promise. The whole project builds
  with the Go standard library only; new `require` lines need a very good reason.
- Never commit real DeepSeek tokens, proxy keys, or `accounts.txt`. It is
  already ignored by `.gitignore`.
- One focused change per pull request.

## Getting started

```bash
git clone https://github.com/0xgetz/deepseek2api.git
cd deepseek2api
go build ./...
go vet ./...
go test ./...
```

There is no CI: run the three commands above locally before opening a PR. A
change that does not build, vet clean and pass tests will not be merged.

## Style

- `gofmt` is mandatory (`gofmt -w .`).
- Prefer small functions and explicit error handling.
- Comments explain *why*, not *what*.
- Match the existing naming and package layout (`config`, `handlers`, `deepseek`).

## Pull requests

1. Fork the repo and create a topic branch.
2. Make the change, add or update tests where it makes sense.
3. Run `gofmt -w .`, `go vet ./...`, `go test ./...`.
4. Describe the problem and the fix in the PR body. Link related issues.

## Reporting bugs

Open an issue with the upstream response, your request (redacting tokens),
and the exact command you ran. Logs go a long way.
