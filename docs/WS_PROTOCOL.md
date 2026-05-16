# Phaze WebSocket protocol (`NexusMessage`)

All clients connect to **`GET /ws`** and exchange JSON objects matching **`NexusMessage`** (`nexus_server/main.go`). Field names use **snake_case** in JSON where noted.

## Envelope shape

| Field | JSON | Purpose |
|-------|------|---------|
| `Type` | `type` | Message discriminator (see below). |
| `Sender` | `sender` | Set by **server** for authenticated traffic; do not trust inbound `sender` before auth. |
| `Recipient` | `recipient` | DM / signal target username. |
| `Body` | `body` | Password (auth), chat text or **E2EE hex**, verification codes, etc. |
| `Status` | `status` | Result codes, presence string, abuse reason tag. |
| `Results` | `results` | `[]string` (search hits, pending friend usernames, blocks). |
| `SDP` / `Candidate` | `sdp`, `candidate` | WebRTC signaling (often **E2EE-wrapped** like `body`). |
| `Token` | `token` | Misc tokens. |
| `Error` | `error` | Human-readable error. |
| `Email` | `email` | Registration / forgot-password. |
| `Mood` / `DisplayName` | `mood`, `display_name` | Profile. |
| `ConvoID` / `ConvoName` | `convo_id`, `convo_name` | Groups. |
| `Members` | `members` | Group member list. |
| `TurnConfig` | `turn_config` | TURN REST-style credentials from relay. |
| `TOTPCode` / `TOTPURI` | `totp_code`, `totp_uri` | 2FA. |
| `QRToken` / `QRData` | `qr_token`, `qr_data` | Session resume + QR login. |
| `DeviceInfo` | `device_info` | Session metadata. |
| `Envelopes` | `envelopes` | `map[username]ciphertext` for `convo_msg` group E2EE. |
| `PublicKey` | `public_key` | 32-byte Curve25519 public key (array in JSON). |
| `KeyFingerprint` | `key_fingerprint` | Short SHA-256 prefix hex (TOFU). |

## E2EE body format (pairwise NaCl `box`)

When both peers have exchanged keys, sensitive string fields use:

```text
E2EE:<hex( nonce24 || box_ciphertext )>
```

Where `box_ciphertext` is NaCl box with the 24-byte nonce (same as Go `golang.org/x/crypto/nacl/box`).

## Inbound `type` values handled by Nexus (relay)

Implemented in the main `switch` in `nexus_server/main.go` (authoritative list):

- **`pstn_call`** — only honored when **`PHAZE_ENABLE_PSTN=true`** on Nexus; otherwise the client receives `pstn_status` with an error (use WebRTC in chat). When enabled, still requires phone verification as implemented in code.
- All other inbound types: `register`, `verify_email`, `status_update`, `request_phone_link`, `verify_phone_link`, `update_profile`, `auth`, `session_auth`, `revoke_session`, `resend_verification`, `enable_totp`, `confirm_totp`, `disable_totp`, `forgot_password`, `reset_password`, `qr_login_create`, `qr_login_approve`, `qr_login_check`, `msg`, `block`, `unblock`, `list_blocks`, `report_abuse`, `typing`, `presence`, `search`, `friend_request`, `friend_accept`, `friend_reject`, `friend_remove`, `convo_create`, `convo_msg`, `convo_leave`, `read_receipt`, **`key_request`** (forwarded friend→friend), `call_offer`, `call_answer`, `ice_candidate`, `call_reject`, `call_end`.

### `presence` (two behaviors)

1. **Roster status** — updates the sender’s status string and is **broadcast** to accepted friends as `{ type, sender, status }` (no keys).
2. **Directed public key** — if `recipient` is set, `public_key` is exactly **32 bytes** (JSON base64 string from Go, or equivalent), and the pair are **accepted friends**, Nexus **forwards the full message** to that recipient (used to answer `key_request` with NaCl `public_key` + `key_fingerprint`).

Unknown types are logged and dropped.

## Common outbound types (examples)

`auth_result`, `register_result`, `verify_result`, `friend_status`, `friend_request`, `friend_accepted`, `pending_requests`, `presence`, `msg`, `msg_status`, `search_results`, `typing`, `read_receipt`, `kicked`, `call_error`, `convo_info`, `convo_created`, `convo_error`, `blocks`, `block_result`, `report_result`, `totp_result`, etc.

## Session resume

After successful `auth`, server returns `auth_result` with `status: "ok"` and **`qr_token`** = opaque session string. Client should send:

```json
{ "type": "session_auth", "qr_token": "<same token>" }
```

on the next WebSocket before other authenticated actions.

## CORS / origins

When `Phaze_ALLOWED_ORIGINS` is **unset**, WebSocket origins are **allowed from anywhere** (dev-friendly). In production, set a comma-separated allowlist of exact `Origin` header values (e.g. `https://app.phazechat.world`).
