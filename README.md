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
| **UI** | Fyne native desktop (OpenGL/Metal). Android APK via fyne-cross |

## What does not work yet

- **Opus audio** — still PCMU; desktop quality is phone-era, not Skype-era
- **No federation** — single-relay topology; `phazechat.world` is the only signaling host in prod
- **Asset vault key is in source** — the master key for `assets.vault` is compiled in; symbolic protection only
- **No production installers** — no MSI, no pkg, no AppImage, no auto-update channel
- **Windows VP8** — mingw cross-build can't link libvpx without building it from source first; Windows client still uses JPEG video
- **Mobile interop with new group E2EE** is untested on a real device (S23 ↔ desktop verification is the next gate)
- **No metrics/health endpoints** on the relay; no structured logging

---

## Build

**Relay (Phaze Nexus):**
```bash
cd nexus_server
go build -o phaze-nexus
./phaze-nexus
```

**Desktop client:**
```bash
cd native_client
# Linux/macOS: libvpx-dev required for VP8 video
go build -o phaze
./phaze
```

**Android APK:**
```bash
cd native_client
fyne-cross android -app-id world.phazechat.client
```

## TURN

Drop `scripts/phaze_turnserver.conf` into your coturn install, then set on the relay:

```
PHAZE_TURN_SECRET=<openssl rand -hex 32>
PHAZE_TURN_URL=turn:turn.phazechat.world:3478
PHAZE_TURN_SHORT_TERM=true
```

Secret must match the CoTURN `static-auth-secret`.

---

## Threat model (short version)

- The relay is honest-but-curious. Server operators cannot read message bodies, call SDP, or ICE candidates — all E2EE wrapped before hitting the wire.
- TOFU key pinning means the **first** handshake with an unknown peer is vulnerable to an active MITM at the relay. Subsequent sessions are pinned.
- At-rest DB encryption protects a stolen laptop from casual inspection. It does **not** protect against malware running as your user.
- No forward secrecy on group messages yet — compromising a long-term key lets an attacker decrypt historical envelopes.

If any of those assumptions are dealbreakers, Phaze is not the right tool for your threat model yet.

---

## Contributing

Bug reports and real-device test reports are more valuable than code right now. If you got through registration → contact add → call → group chat and something broke, that's the issue we want.

---

*Phaze is an independent project. Not affiliated with Microsoft Corporation or the Skype brand.*
