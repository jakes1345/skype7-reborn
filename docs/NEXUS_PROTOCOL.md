# Nexus relay — HTTP routes and WebSocket protocol

Phaze clients talk to **Nexus** over **WebSocket** (`/ws`) using JSON messages shaped like `NexusMessage` in `nexus_server/main.go`. Chat operations are **WebSocket-first**; HTTP covers static pages, a few JSON endpoints, and the **browser pilot** at `/web/`.

## HTTP routes (operator / integration)

| Method | Path | Purpose |
|--------|------|--------|
| GET | `/` | Marketing landing |
| GET | `/download`, `/features`, … | Marketing / legal pages |
| GET | `/downloads/…` | Static mirror binaries under `public/downloads/` |
| GET | `/web/…` | Browser pilot (`public/web/`) — NaCl-compatible chat UI |
| GET | `/reset` | Password reset flow |
| GET | `/version`, `/api/v1/version` | Version JSON |
| GET | `/health` | JSON health (DB, TURN flags, connected clients) |
| GET | `/api/v1/stats` | Public stats |
| GET | `/api/v1/profile/…`, `/api/v1/avatars/…` | Profile / avatar bytes |
| GET | `/public/…` | Other static files under `public/` |
| POST | `/twiml/outbound` | Twilio PSTN webhook (operator) |
| GET | `/robots.txt`, `/sitemap.xml` | SEO |

**Public HTTP API (OpenAPI):** [openapi/nexus-http.yaml](./openapi/nexus-http.yaml) documents only the small JSON surface; it does **not** describe WebSocket types.

## WebSocket: `NexusMessage`

Key fields: `type`, `sender`, `recipient`, `body`, `status`, `public_key` (base64-encoded NaCl public key), `key_fingerprint` (SHA-256 of pubkey, first 8 bytes as hex — matches `native_client/internal/crypto.Fingerprint`).

## Pairwise keys on the relay

- **`presence`**: After login, clients should send `presence` with `status` and optional `public_key` / `key_fingerprint`. Nexus **fans out** that presence to **accepted friends** and includes `public_key` when provided (so browsers and desktops learn keys without trusting the server for key correctness — TOFU is enforced in the client).
- **`presence` with `recipient` set**: Direct delivery to one online user (used when answering a `key_request` with a public key).
- **`key_request`**: Forwarded to `recipient` if online, with `sender` stamped to the authenticated session. Native clients use this to ask a peer to re-send their public key material.

## E2EE message body format (1:1)

When a NaCl public key is known for the peer, the native client wraps sensitive fields as:

`E2EE:` + lowercase hex of `nonce(24) || box_ciphertext` where `box` is NaCl box with that nonce (same as `golang.org/x/crypto/nacl/box` `Seal`).

## Versioning

There is **no wire version field** yet. Pin integrations to a **git tag** and diff `NexusMessage` plus the server `switch msg.Type` when upgrading.
