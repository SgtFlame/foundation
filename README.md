# Foundation
Open Source Veritas AI Foundation 

**Veritas** is my "The Right Answer" architecture and collection of projects.

This **Foundation** mono-repo is a catch-all, highly experimental code base for both Axiom, Axiom Core, and the corresponding server components.

## Getting Started

These steps assume you have just cloned the repo and installed everything in [DEPENDENCIES.md](DEPENDENCIES.md). Commands are run from the repo root unless they say otherwise.

### 1. Set your environment

Environment variables live in `backend/scripts/setEnv.sh`. It is in `.gitignore`, so your keys are never committed. Make it from the template:

```bash
cp backend/scripts/setEnv.example.sh backend/scripts/setEnv.sh
```

Then edit `backend/scripts/setEnv.sh` and set `GEMINI_API_KEY` to a key from [Google AI Studio](https://aistudio.google.com/apikey). Without a key the server still runs, but chat fails and nothing is indexed. You can also set a key later in Settings › AI › Providers. The other variables in the file are optional and commented out.

`serve.sh` sources `setEnv.sh` on every start. To use it with other commands in your own shell, run `source backend/scripts/setEnv.sh`.

### 2. Start the backend

```bash
cd backend
./scripts/serve.sh
```

This builds `backend/out/cli`, then runs the server on port 8080. It rebuilds and restarts the server whenever a Go file changes.

The **first build takes 20-40 minutes** and several GB of disk, because it compiles LanceDB's native library from Rust. Later builds take seconds. If the linker gets killed for lack of memory (as in a 7 GB codespace), uncomment the `GOGC` and `GOFLAGS` lines in `setEnv.sh` and run it again.
### 3. Start the frontend

In a second terminal (or a tmux pane):

```bash
cd frontend
yarn
cd example
yarn dev
```

Vite serves the app on port 5173 and passes `/api/v1` requests through to the backend on port 8080.

### 4. Open the app

Go to port 5173 in your browser (typically http://localhost:5173, though your setup may differ; in a Codespace, use the forwarded port).

The app starts in **single-user mode**, with no sign-in. To create accounts, choose *Set up accounts…* from the account menu at the bottom of the side bar. This creates the first user, who becomes *Super Administrator*. After setup, everyone must sign in.

Data is kept in `~/.foundation` (the databases, the LanceDB index and the knowledge base). To change where, pass `--data-dir <dir>` to `serve.sh`.

### Optional extras

- **Log storage**: `cd backend && ./scripts/victorialogs.sh` runs VictoriaLogs on port 9428. Turn it on in Settings › System › Logging, or set `FOUNDATION_VLOGS_URL` in `setEnv.sh`.
- **Vault**: to keep secrets in HashiCorp Vault instead of `~/.foundation/secret.key`, see [documentation/Secrets.md](documentation/Secrets.md).

I have only tested this within a GitHub Codespace.

Contact me [here](https://discord.gg/6QgrMU53) for feedback, bug reports, etc.  Pull requests and feature requests are welcome!
