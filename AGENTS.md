# AGENTS.md

## Cursor Cloud specific instructions

### Architecture

Phaze is a Go workspace (`go.work`) with two modules:
- `nexus_server/` — WebSocket relay server (pure Go, no CGO, uses modernc.org/sqlite)
- `native_client/` — Fyne-based desktop GUI client (requires system GL/X11/audio/vpx libs)

### Running the server

```bash
cd nexus_server && go build -o phaze-nexus && DB_PATH=./nexus.db PORT=8080 ./phaze-nexus
```

The server auto-creates its SQLite database. No external DB service needed.

### Running tests

Tests must be run per-module (not from workspace root with `./...`):
```bash
cd /workspace/nexus_server && go test -v ./...
cd /workspace/native_client && go test ./...
```

### Lint

```bash
go vet ./nexus_server/...
go vet ./native_client/...
```

### Key gotchas

- The Go workspace pattern means `go test ./...` from the repo root fails — always `cd` into a module first.
- The native client requires `libvpx-dev` in addition to the standard Fyne GL/X11 dependencies. Without it, the build fails on the `pion/mediadevices` VP8 codec.
- Registration uses `type: "register"` with `sender` (username) and `body` (password) fields; authentication uses `type: "auth"` with the same field mapping.
- The `run_local_phaze.sh` script uses `pkill -f` which is unsafe for agents — prefer building and running the server directly.
- No SMTP server is configured in dev; registration returns `status: "pending_verification"` and the OTP code is logged server-side. For testing, set `is_verified = 1` directly in the SQLite DB.
