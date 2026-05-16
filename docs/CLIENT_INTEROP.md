# Web, desktop, and mobile — working together

All Phaze clients are designed around **one relay** (Nexus) and **one account database** (`nexus_server` SQLite by default). If every client points at the **same Nexus URL** and speaks the **same `NexusMessage` wire types**, users see one roster, one friend graph, and one chat history model (server-side offline queue + client-side logs where applicable).

## What is already shared

| Capability | Nexus | Desktop | Android (Fyne) | Web (`web/`) |
|-------------|-------|---------|----------------|---------------|
| Same WS JSON (`NexusMessage`) | Yes | Yes | Yes (same binary as desktop) | Yes |
| `auth` / `session_auth` / TOTP | Yes | Yes | Yes | Yes |
| Friends, presence, `msg` relay | Yes | Yes | Yes | Yes |
| Pairwise E2EE (`E2EE:` + NaCl box) + `key_request` / directed `presence` keys | Yes | Yes | Yes | Yes |
| Group `convo_*` + `envelopes` | Yes | Yes | Yes | UI not in web yet |
| WebRTC signaling (`call_*`, `ice_*`) | Forwards | Pion | Pion | Not wired in web yet |

**Desktop and mobile** today are the **same codebase** (`native_client`): Android/iOS packages are built with **`fyne package`** (see `docs/MOBILE_BUILDS.md` and the `Makefile` `android` / `ios` targets). They are not a separate protocol fork.

## How to run them “together” in practice

1. **Run one Nexus** (or use your deployed `wss://…/ws`).
2. **Point every client at that gateway**
   - Desktop/mobile: `PhazeInfra.Gateway` / server picker / `ws://localhost:8080/ws` for local dev.
   - Web: `VITE_NEXUS_WS` in `.env.local` (see `web/.env.example`).
3. **CORS**: for browser access, set `Phaze_ALLOWED_ORIGINS` on Nexus to your web origin (omit in dev to allow all origins, per server code).
4. **Same username** everywhere = same account; **session tokens** issued at login work for `session_auth` on any surface (including web).

## Known gaps (interop you should plan for)

1. **Voice/video web ↔ native** — Native registers **PCMU** (and VP8/…) in Pion; browsers usually prefer **Opus**. Until the web app implements `RTCPeerConnection` **and** you align codecs (Opus everywhere, or negotiated PCMU where supported), **calls** are native↔native; **chat** is already cross-platform.
2. **Web registration** — Server supports `register` / `verify_email`; the web UI still assumes you can log in (e.g. after registering on desktop). Adding the full wizard on web removes that friction.
3. **Local keys** — Each installation has its **own** NaCl keypair (desktop: SQLite `Accounts`; web: `localStorage`). That is normal: TOFU pins are per device. Same user on phone and laptop = **two device identities** for crypto unless you add explicit key export/import later.

## Single sentence

**Chat and social features already interoperate across web, desktop, and Android via Nexus; finish web WebRTC + codec alignment for calls and add web registration to match desktop onboarding.**
