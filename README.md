# Phaze

A sovereign Skype-style chat/voice/video client and relay, written in Go.

Live at **[phazechat.world](https://phazechat.world)**.

> **Status: pre-1.0, unverified in the field.** Builds run end-to-end on the maintainer's box, but third-party real-device testing has not happened yet. If you try it and something breaks, open an issue — that's the whole point of where we are right now.

---

## What actually works today

| Area | State |
|---|---|
| **Accounts** | Email OTP registration, password reset, TOTP 2FA enrol/disable, 30-day session tokens, cross-device QR sign-in |
| **Messaging 1:1** | Pairwise E2EE via NaCl box (Curve25519 + XSalsa20 + Poly1305) with TOFU key pinning; offline delivery |
| **Group chats** | Per-member envelope fan-out (MLS-lite): client encrypts body once per recipient, server never sees plaintext |
| **Voice calls** | PCMU (G.711 µ-law) over RTP via Pion WebRTC. Pure Go, no CGO for audio |
| **Video calls** | VP8 over RTP on desktop (Linux/macOS) via `pion/mediadevices` + libvpx. Android still falls back to JPEG-over-DataChannel |
| **Local DB** | SQLite encrypted at rest with AES-256-GCM; key lives in the OS keyring, not on disk |
| **Relay discovery** | Three layers: Phaze Nexus websocket relay → libp2p Kademlia DHT → mDNS LAN mesh |
| **TURN** | CoTURN with per-call HMAC-SHA1 short-term creds |
| **File transfer** | WebRTC DataChannel, peer-to-peer |
| **UI** | Fyne native desktop (OpenGL/Metal). Android APK / iOS via **`fyne package`** (see `docs/MOBILE_BUILDS.md`). **Web beta** (`web/`) — auth, friends, 1:1 chat, NaCl E2EE + key handoff via Nexus |

## What does not work yet

- **Opus audio** — still PCMU; desktop quality is phone-era, not Skype-era
- **No federation** — single-relay topology; `phazechat.world` is the only signaling host in prod
- **Asset vault key is in source** — the master key for `assets.vault` is compiled in; symbolic protection only
- **No production installers** — no MSI, no pkg, no AppImage, no auto-update channel
- **Windows VP8** — mingw cross-build can't link libvpx without building it from source first; Windows client still uses JPEG video
- **Mobile interop with new group E2EE** is untested on a real device (S23 ↔ desktop verification is the next gate)
- **Limited observability** — `/health` and `/api/v1/stats` exist on the relay, but there is no structured logging, dashboards, or alerting pipeline yet

---

## Build

**Full local build (tests + relay + desktop + web + Android APK):**
```bash
make build-all    # tests, then bin/phaze-nexus, bin/Phaze, bin/Phaze-android-arm64.apk, and web/dist/
make test         # Go tests in nexus_server + native_client only
```

Pushes to **GitHub** also run **Actions** (Linux + macOS) — see **[GitHub (source of truth)](#github-source-of-truth)** below.

**Relay (Phaze Nexus):**
```bash
cd nexus_server
go build -o phaze-nexus .
./phaze-nexus
```

From the repo root you can also run `make nexus` and then `./bin/phaze-nexus`. The server calls `resolveWorkingDir()` at startup so `templates/` and `public/` are found when the binary sits in `bin/` next to `nexus_server/`. For unusual layouts, set **`PHAZE_ASSET_ROOT`** to the directory that contains those folders.

**Desktop client:**
```bash
cd native_client
# Linux/macOS: libvpx-dev required for VP8 video
go build -o phaze .
./phaze
```

**Web client (beta):**
```bash
cd nexus_server && go build -o phaze-nexus . && ./phaze-nexus
# other terminal:
cd web && cp .env.example .env.local && npm install && npm run dev
```
Set `VITE_NEXUS_WS` in `.env.local` to your relay (for example `ws://127.0.0.1:8080/ws`). In production, add your HTTPS origin to `Phaze_ALLOWED_ORIGINS` on Nexus so browser WebSockets pass the origin check.

Wire protocol: `docs/WS_PROTOCOL.md`. Roadmap: `PHAZE_ENGINEERING_GUIDELINE.md`. **Web + desktop + Android together:** `docs/CLIENT_INTEROP.md` (same Nexus; chat/E2EE aligned; calls need web WebRTC + codec work).

**Self-host on phazechat.world (no Fly.io):** see **`docs/DEPLOY_SELF_HOSTED.md`** and `nexus_server/docker-compose.yml` — DNS to your VPS, TLS on Caddy/nginx, Nexus behind reverse proxy.

**Mobile (Android / iOS / macOS):** **`docs/MOBILE_BUILDS.md`** — the **IDE** (e.g. Linux `.tar.gz` under `…/android-studio`) is **not** `ANDROID_HOME`; the **SDK** usually lives under **`$HOME/Android/Sdk`**. Copy **`local.mk.example`** → **`local.mk`** to pin paths; install **NDK** in SDK Manager, then `make android` → `bin/Phaze-android-arm64.apk`. iOS: **`make ios`** / **`make iossim`** on **macOS + Xcode** only.

```bash
export ANDROID_HOME="$HOME/Android/Sdk"
make android
```

## TURN

Drop `scripts/phaze_turnserver.conf` into your coturn install, then set on the relay:

```
PHAZE_TURN_SECRET=<openssl rand -hex 32>
PHAZE_TURN_URL=turn:turn.phazechat.world:3478
PHAZE_TURN_SHORT_TERM=true
```

Secret must match the CoTURN `static-auth-secret`.

## PSTN (optional phone bridge)

By default **PSTN is off** on Nexus — voice/video is **WebRTC between Phaze users** only (no Twilio call charges). See **`docs/WEBRTC_AND_PSTN.md`**.

- **`PHAZE_ENABLE_PSTN=true`** on Nexus: allow `pstn_call` through to Twilio when `TWILIO_*` and `Phaze_APP_URL` are set.
- **`PHAZE_ENABLE_PSTN=true`** on the desktop client: show the numeric **Dial** tab again.

---

## Threat model (short version)

- The relay is honest-but-curious. Server operators cannot read message bodies, call SDP, or ICE candidates — all E2EE wrapped before hitting the wire.
- TOFU key pinning means the **first** handshake with an unknown peer is vulnerable to an active MITM at the relay. Subsequent sessions are pinned.
- At-rest DB encryption protects a stolen laptop from casual inspection. It does **not** protect against malware running as your user.
- No forward secrecy on group messages yet — compromising a long-term key lets an attacker decrypt historical envelopes.

If any of those assumptions are dealbreakers, Phaze is not the right tool for your threat model yet.

---

## GitHub (source of truth)

- **Repository:** [github.com/jakes1345/skype7-reborn](https://github.com/jakes1345/skype7-reborn) — issues, PRs, and history all live here.
- **CI:** [Actions](https://github.com/jakes1345/skype7-reborn/actions) on **`main` / `master`**  
  - **Phaze CI** (Ubuntu): `go build` native + Nexus, Go tests, `npm ci` + web production build.  
  - **Sovereign Apple Verification** (macOS): desktop binary + **iOS Simulator** Fyne package — this is the “we don’t have a physical Mac” path for **automated** Apple-side compile checks.  
- **Local vs CI:** `make build-all` on your machine also produces the **Android APK**; CI today does **not** run the full Android NDK pipeline (that stays local unless we add a dedicated job).

---

## Contributing

Bug reports and real-device test reports are more valuable than code right now. If you got through registration → contact add → call → group chat and something broke, that's the issue we want.

---

*Phaze is an independent project. Not affiliated with Microsoft Corporation or the Skype brand.*
