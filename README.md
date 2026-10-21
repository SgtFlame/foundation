# Foundation
Open Source Veritas AI Foundation 

**Veritas** is my "The Right Answer" architecture and collection of projects.

This **Foundation** mono-repo is a catch-all, highly experimental code base for both Axiom, Axiom Core, and the corresponding server components.

## Getting Started

Clone this repo, and in VS Code (or whatever terminal you use for Bash), then run the following commands in two separate terminal windows or use tmux or whatever.

Front end:

```bash
cd frontend/axiom
yarn
yarn dev
```

Back end:

```bash
cd backend
go run cli/main.go serve
```

Then navigate to port 5173 in your browser (typically http://localhost:5173 but your setup might be different).

I have only tested this within a GitHub Codespace.

Contact me [here](https://discord.gg/6QgrMU53) for feedback, bug reports, etc.  Pull requests and feature requests are welcome!
