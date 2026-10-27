# Foundation Dependencies

## Claude

```bash
curl -fsSL https://claude.ai/install.sh | bash
```

## Others

```bash
go install github.com/spf13/cobra-cli@latest
```
## Backend

`backend/scripts/build.sh` installs what it needs on first use, but these can be installed ahead of time:

- **Rust** (rustup) and **protoc**: build LanceDB's native library from lancedb-go's Rust sources (`build.sh` installs rustup into `~/.cargo` and protoc into `backend/third_party/protoc/` if missing). The first build takes 20-40 minutes and several GB; set `CARGO_TARGET_DIR` to a roomy disk.
- **gcc** (or clang): CGO.
- **inotify-tools** (`inotifywait`): file watching for `scripts/serve.sh`; without it serve.sh polls every second.

```bash
sudo apt-get install -y build-essential inotify-tools
```

- **`GEMINI_API_KEY`**: the Gemini API key for the AI services (or `serve --gemini-api-key`).

- **VictoriaLogs** (optional): log storage for the Log Explorer. `backend/scripts/victorialogs.sh` downloads the pinned release into `backend/third_party/victorialogs/` on first use and runs it on `:9428`; then turn it on in Settings › System › Logging, or pass `serve --vlogs-url http://localhost:9428`. Without it, logs go to stderr only.
