#!/usr/bin/env bash
# Environment for the backend scripts. Copy it to setEnv.sh (gitignored, so
# keys stay out of the repo) and fill it in; serve.sh sources setEnv.sh if
# it exists. To use it in your own shell: source scripts/setEnv.sh

# Gemini API key from Google AI Studio. Wins over the key stored in Settings
# (AI › Providers); without either, AI calls fail and nothing is indexed.
export GEMINI_API_KEY=

# Optional. Uncomment the ones you need; serve's flags win over all of them.

# Key that encrypts secret settings without Vault (default: <data-dir>/secret.key,
# created on first use).
# export FOUNDATION_SECRET_KEY=

# VictoriaLogs to store logs in (see scripts/victorialogs.sh).
# export FOUNDATION_VLOGS_URL=http://localhost:9428

# Vault holding the server's secrets; serve then starts sealed
# (documentation/Secrets.md).
# export FOUNDATION_VAULT_ADDR=http://127.0.0.1:8200
# export FOUNDATION_VAULT_ROLE_ID=

# Where memory is short (a 7 GB codespace), link without debug symbols so
# the linker is not killed.
# export GOGC=25
# export GOFLAGS=-ldflags=-s
