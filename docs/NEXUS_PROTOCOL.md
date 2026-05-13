# Nexus relay — HTTP routes and WebSocket protocol

Phaze clients talk to **Nexus** over **WebSocket** (`/ws`) using JSON messages shaped like `NexusMessage` in `nexus_server/main.go`. There is **no stable public REST API** for all chat operations today; integrations should assume **WS-first**, with a small HTTP surface for static assets and a few JSON endpoints.

## HTTP routes (operator / integration)

| Method | Path | Purpose |
|--------|------|--------|
| GET | `/` | Marketing landing (`templates/landing.html` when present) |
| GET | `/download`, `/features`, `/rates`, `/about`, `/support`, `/privacy`, `/terms`, `/legal` | Static marketing / legal pages |
| GET | `/downloads/…` | Static mirror binaries under `public/downloads/` |
| GET | `/reset` | Password reset flow (token query / POST) |
| GET | `/version`, `/api/v1/version` | Client-visible version string |
| GET | `/health` | JSON health (DB, TURN flags, connected clients) |
| GET | `/api/v1/stats` | Limited public stats (for landing counters) |
| GET | `/api/v1/profile/{user}` | Public profile snippet (if implemented for path) |
| GET | `/api/v1/avatars/…` | Avatar bytes |
| GET | `/public/…` | Static files under `public/` |
| GET | `/web/…` | **Web client (developer preview)** under `public/web/` |
| POST | `/twiml/outbound` | Twilio PSTN webhook (operator) |
| GET | `/robots.txt`, `/sitemap.xml` | SEO |

**WebSocket:** `GET /ws` — after upgrade, all application traffic is JSON `NexusMessage` objects (see below). Rate limits apply to the upgrade; per-connection message throttling exists on the read loop.

## `NexusMessage` JSON shape (shared with clients)

Fields are optional unless a given `type` requires them:

| Field | Meaning |
|-------|--------|
| `type` | Discriminator (required) |
| `sender`, `recipient` | Phaze usernames |
| `body` | Payload (plaintext legacy or ciphertext blob) |
| `status`, `error` | Human-readable state / errors |
| `results` | String arrays (search, blocks, …) |
| `sdp`, `candidate` | WebRTC signaling |
| `token`, `email`, `totp_code`, `totp_uri` | Auth / TOTP |
| `convo_id`, `convo_name`, `members` | Group conversations |
| `envelopes` | Map username → ciphertext for group E2EE (`convo_msg`) |
| `public_key`, `key_fingerprint` | TOFU / keying metadata |
| `turn_config` | TURN credentials pushed to clients |
| `qr_token`, `qr_data`, `device_info` | QR login + device labeling |

Exact struct: `nexus_server/main.go` → `type NexusMessage struct`.

## Inbound WebSocket `type` values (client → server)

Handled in the main `switch msg.Type` loop (unauthenticated types run before auth; most require an authenticated session):

**Account & auth**

- `register`, `verify_email`, `auth`, `session_auth`, `revoke_session`
- `resend_verification`, `forgot_password`, `reset_password`
- `enable_totp`, `confirm_totp`, `disable_totp`
- `qr_login_create`, `qr_login_approve`, `qr_login_check`

**Profile & phone**

- `update_profile`, `status_update`
- `request_phone_link`, `verify_phone_link`

**Social graph**

- `friend_request`, `friend_accept`, `friend_reject`, `friend_remove`
- `block`, `unblock`, `list_blocks`, `report_abuse`

**Messaging**

- `msg` — 1:1 message (E2EE payload in `body` from client perspective)
- `typing`, `presence`, `search`
- `read_receipt`

**Group chat**

- `convo_create`, `convo_msg`, `convo_leave`

**WebRTC signaling**

- `call_offer`, `call_answer`, `ice_candidate`, `call_reject`, `call_end`

**PSTN (when bridge configured)**

- `pstn_call`

## Outbound / fan-out examples (server → client)

Not exhaustive — see server code for the full set:

- `auth_result`, `register_result`, `verify_result`, `kicked`
- `msg`, `msg_status`, `typing`, `presence`, `search_results`
- `friend_request`, `friend_accepted`, `friend_removed`
- `convo_created`, `convo_msg`, `convo_left`, `convo_error`
- `call_error`, passthrough `call_offer` / `call_answer` / `ice_candidate`
- `totp_result`, `qr_login_result`, `forgot_password_result`, `reset_password_result`
- `pstn_status`, `report_result`, `block_result`, `blocks`

## Building a **web client**

Today the **native Fyne client** implements NaCl E2EE, SQLite, keyring, WebRTC, and the full UX. A browser client must either:

1. **Reuse crypto in WebAssembly** (Go → WASM or a JS NaCl port) and implement the same envelope rules, or  
2. Ship a **non-E2EE or trust-relay** mode (not current Phaze security story) — unacceptable without explicit user consent.

The `/web/` static preview exists to exercise **connectivity and protocol literacy** only; it is **not** a replacement for the native app.

## Versioning

There is **no wire version field** yet. Treat the protocol as **implicitly locked** to the `phaze-nexus` binary you deploy. For third parties, pin to a **git tag** and diff `NexusMessage` + the `switch msg.Type` block when upgrading.
