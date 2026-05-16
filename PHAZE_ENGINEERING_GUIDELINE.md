# Phaze — engineering guideline & forward plan

This document is the **single place** for “what we have,” “what we deliberately did not line-by-line read,” and **how to ship the web client + harden everything else** without thrashing.

---

## 0) Repo scope (product-only)

Legacy Skype reverse-engineering trees (**`dissection/`**, **`oluo3y/`**, Ghidra exports, installer dumps) are **not** part of this repository. The product surface is:

| Area | Treatment |
|------|-------------|
| **`native_client/third_party/**`** (vendored `mobile_x`, `anet`, …) | **Upstream toolchains** for mobile builds — touch only when debugging those pipelines. |
| **`native_client/main.go`** (~2.7k lines) | Large; behavior is spread across **`internal/ui/*`** and **`internal/chat/*`**. |
| **Every `internal/ui/*.go` file** | **First-class** for UX parity when extending the web client (flows, not Fyne widgets). |

**Reference reads for this guideline:** root `README.md`, `nexus_server/main.go`, `nexus_server/integration_test.go`, both `go.mod`, `native_client/internal/crypto/e2ee.go`, `internal/chat/webrtc.go` / `audio.go`, `internal/chat/p2p.go` (in part), `internal/sentinel/networking.go` (in part).

---

## 1) Product snapshot (authoritative: code + README)

### 1.1 What Phaze is

- **Sovereign** Skype-style **chat + voice + video** with a **Go relay** (`nexus_server`) and **Go + Fyne** desktop/Android client (`native_client`).
- **E2EE:** NaCl `box` for 1:1; group messages use **per-member ciphertext** (`Envelopes` on `NexusMessage`) so the relay never sees plaintext bodies.
- **Calls:** **Pion WebRTC v3**; audio is **PCMU (8 kHz)** in `internal/chat/audio.go`; video **VP8** where libvpx path exists; Android video fallback per README.

### 1.2 Relay (`nexus_server/main.go`) — facts that matter for *all* clients

- **WebSocket:** `GET /ws` — JSON messages match **`NexusMessage`** (same shape as `native_client/main.go`).
- **Auth paths:** `auth`, `session_auth`, TOTP enroll/confirm/disable, password reset, QR login create/approve/check, `revoke_session`.
- **Social graph:** friends, blocks, abuse reports, search, typing, read receipts (friends-only gate).
- **Groups:** `convo_create`, `convo_msg` (envelope-first fanout), `convo_leave`, offline storage for convo messages.
- **WebRTC signaling:** `call_offer`, `call_answer`, `ice_candidate`, `call_reject`, `call_end` — forwarded if recipient online.
- **TURN:** `TurnConfig` pushed on successful auth (`generateMediaToken`).
- **HTTP surface:** landing/download/features, `/health`, `/api/v1/stats`, `/api/v1/version`, `/version`, avatars, profile, static `public/`, optional Twilio SMS + PSTN (PSTN gated by `PHAZE_ENABLE_PSTN`), CORS via `Phaze_ALLOWED_ORIGINS`, rate limits on HTTP upgrade + per-WS message limiter.

### 1.3 Desktop client (`native_client`)

- **Gateway:** `PhazeInfra.Gateway` defaults to `wss://phazechat.world/ws` (see `main.go` `Infrastructure`).
- **Crypto module:** small, clear **`nacl/box`** wrapper (`internal/crypto/e2ee.go`) — web must **reimplement the same wire bytes**, not reinterpret strings.
- **WebRTC helper:** `internal/chat/webrtc.go` — `iceServers()` prioritizes Nexus `TurnConfig`, then env `SHADOW_TURN_*`, then **Metered open relay** defaults (document for web: same TURN policy or explicit env).
- **P2P:** `internal/chat/p2p.go` — libp2p host + DHT + `/phaze/signal/1.0.0`; web client likely **skips or stubs** until you define a WASM/JS story — **do not block web v1 on full libp2p parity**.

---

## 2) Strategic decisions (locked for the next 90 days)

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Web client language** | **TypeScript** | Browser reality; largest ecosystem for WebRTC + strict typing. |
| **Web UI framework** | **SvelteKit *or* React + Vite** (pick one in week 1) | SvelteKit = less boilerplate; React = more hiring/docs. **Do not** rewrite both. |
| **Web crypto** | **Web Crypto + libsodium-js (or tweetnacl-js)** aligned with **exact** NaCl box format from Go | Interop beats “similar crypto.” |
| **WebRTC in browser** | **browser RTCPeerConnection** + same signaling JSON as desktop | Reuse `call_*` / `ice_*` message types verbatim. |
| **libp2p on web** | **Out of scope for web v1** | Ship signaling + E2EE + calls first; mesh is a later milestone or desktop-only. |

---

## 3) Phased roadmap (engineering order)

### Phase 0 — Contract freeze (3–5 days)

**Goal:** One machine-readable description of the wire protocol so web and Go cannot drift.

1. Extract **`NexusMessage`** into a shared doc: list every `msg.Type` string used in `nexus_server` `switch` + what fields are required. **Done:** `docs/WS_PROTOCOL.md`.
2. Add **`docs/WS_PROTOCOL.md`** — **done** (keep updated when `switch` changes).
3. Extend `nexus_server/integration_test.go` with cases for: `convo_msg` + `Envelopes`, `call_offer` round-trip, `session_auth`, `block` / `msg` drop path. **Partially done:** added `key_request` relay + directed `presence` public-key forward tests; convo_msg / session_auth tests still optional follow-ups.

**Relay fixes for web ↔ desktop E2EE:** `key_request` is now forwarded between friends; `presence` with `recipient` + 32-byte `public_key` is forwarded to that recipient (required for NaCl key handoff — previously only a stripped broadcast ran).

**Deliverable:** CI runs `go test ./...` green on relay + native internal tests + **`web` production build** (`npm ci && npm run build`).

### Phase 1 — Web shell + auth (1–2 weeks)

**Goal:** Log in, session resume, see roster/presence; **no** WebRTC yet.

**Progress:** `web/` (Vite + React + TypeScript): `session_auth`, password `auth`, TOTP field, friends from `friend_status`, add/accept friends, 1:1 `msg` with NaCl **E2EE:** payloads matching desktop, `key_request` + directed `presence` key exchange, TOFU pins in `localStorage`. **Not yet:** register + `verify_email` wizard in the browser, resilient reconnect loop, WebRTC, group `convo_*` UI.

1. Config: `VITE_NEXUS_WS` (see `web/.env.example`); optional `VITE_NEXUS_HTTP` for future REST helpers.
2. WS client should gain backoff **reconnect** + replay `session_auth` (same pattern as desktop `ReadLoop`).
3. Flows: `register` → `verify_email` → `auth` → store session token; `session_auth` on reload (**token done**; **register UI** still desktop-first).
4. TOTP + QR login: mirror message types (`enable_totp`, `confirm_totp`, `qr_login_*`) in web settings when needed.
5. CORS: set `Phaze_ALLOWED_ORIGINS` in prod to the web origin.

**Deliverable:** Deployed static site (or Nexus-served `web/dist`) where a user can auth and chat with friends — **calls still out of scope for this phase**.

### Phase 2 — 1:1 chat E2EE (1–2 weeks)

**Goal:** Byte-compatible with desktop NaCl payloads.

1. Port **`Encrypt` / `Decrypt` / `Fingerprint`** semantics: 24-byte nonce prefix + box seal/open.
2. Implement friend request / accept / messaging / read receipts / typing using existing `msg.Type` values.
3. Offline: handle `msg_status` `delivered_offline` same as desktop.

**Deliverable:** Web ↔ desktop encrypted DM smoke test documented in `docs/WEB_DESKTOP_INTEROP.md`.

### Phase 3 — WebRTC voice + video (2–4 weeks)

**Goal:** Call setup matches Pion SDP / ICE exchange already on the wire.

1. Map `call_offer`, `call_answer`, `ice_candidate` to `RTCPeerConnection`.
2. Audio: browser **Opus** preferred — negotiate SDP so desktop **PCMU** path either **transcodes** (ideal) or **web sends PCMU** (short-term hack). **Engineering note:** long-term is **Opus everywhere** (see README gap); schedule desktop codec upgrade in parallel if web lands Opus first.
3. Video: VP8/H264 per browser capability; align with desktop VP8 path where possible.
4. TURN: consume `turn_config` from `auth_result` / `session_auth` exactly as `chat.TurnConfig`.

**Deliverable:** Web ↔ desktop voice call; then video.

### Phase 4 — Group chat + envelopes (1–2 weeks)

1. Implement `convo_create`, `convo_info`, `convo_msg` with **`Envelopes`** map: encrypt same plaintext once per member key.
2. Server path already supports offline `convo_msg` insert — verify web handles fan-in UI.

### Phase 5 — “Everything else” from README (parallel tracks)

Pick **one** per release train; do not boil the ocean.

| Track | Work |
|-------|------|
| **Audio quality** | Replace PCMU with **Opus** on desktop + web (SDP + RTP packetizer). |
| **Windows video** | Finish **libvpx** for mingw per README; remove JPEG fallback where possible. |
| **Security** | Move **`assets.vault`** key out of source; rotate story documented. |
| **Ship** | Installers (MSI / AppImage), auto-update consuming `/api/v1/version`. |
| **Ops** | Structured logs (JSON), Prometheus metrics **beyond** `/api/v1/stats`, alert thresholds. |
| **Crypto depth** | Group forward secrecy / MLS roadmap — separate design doc before code. |

---

## 4) Web ↔ Go parity checklist (copy into issues)

- [ ] Same `NexusMessage` JSON field names (`sdp`, `candidate`, `turn_config`, `envelopes`, …).
- [ ] Session token storage: secure (`httpOnly` cookie **or** encrypted local storage — pick and document).
- [ ] ICE: honor Nexus TURN first; avoid hard-coding only openrelay in web prod builds.
- [ ] Block / abuse / search behavior matches server gates.
- [ ] Reconnection: replay presence + pending friend requests + `convo_info` burst like server sends post-auth.
- [ ] Mobile browser constraints: autoplay policy, mic permissions, background tabs.

---

## 5) Repository hygiene (next actions)

1. **Update root `README.md`** — remove or soften “no metrics/health endpoints” ( **`/health`** and **`/api/v1/stats`** exist).
2. **`docs/`** directory — protocol + web interop docs (created in Phase 0–2).
3. **`.github/workflows`** — `go test` for `nexus_server` + `native_client` (skip or matrix Android if too heavy).

---

## 6) Out of scope for this repo

Skype reverse-engineering artifacts, decompiled dumps, and third-party installers belong in a **private** archive if you still need them — not in the Phaze product tree.

---

## 7) Single sentence north star

**Ship polished Nexus + native + web clients on the Phaze protocol, then upgrade media (Opus) and shipping (installers) — without dragging legacy binary archaeology into the repo.**

---

*Update this file when phases complete or decisions change.*
