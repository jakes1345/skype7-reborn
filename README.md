# Phaze™ 7.41 REBORN
### The Sovereign Communication Engine | Bit-Perfect 2014 Forensic Reconstruction

![Phaze Hero](https://github.com/jakes1345/skype7-reborn/blob/master/assets/phaze_hero_mesh.png?raw=true)

**Phaze™** is not a "clone." It is a sovereign, production-grade forensic reconstruction of the classic 7.41 communication experience. We have purged the legacy telemetry, bypassed the centralized shackles, and restored the bit-perfect Aero aesthetic to its rightful place. 

This project is a clean-room engineering effort to provide an unstoppable, P2P-driven communication platform for those who demand absolute privacy, resilience, and high-fidelity nostalgia.

---

## ⚡ The Phaze Pillars

### 🛡️ Sovereign Privacy (E2EE)
Phaze utilizes **End-to-End Encryption (NaCl/Box)** as its native standard. Every message, call, and file transfer is cryptographically sealed at the source using XSalsa20 + Poly1305. The **Phaze Nexus** remains a Zero-Knowledge relay—it never sees anything but encrypted cipher-blobs.

### 🧠 The Phaze Sentinel
Embedded within the core is the **Phaze Sentinel**, an autonomous self-healing agent that monitors connection heartbeats across the mesh. If a relay is seized or the network fragments, the Sentinel autonomously repairs the link or fails over to the **Sovereign DHT**.

### 🌐 Three-Layer Mesh Architecture
Communication is guaranteed through three redundant signaling layers:
1.  **Phaze Nexus**: High-speed sovereign relays (self-hosted at `phazechat.world`) for instant connectivity.
2.  **Sovereign DHT**: Kademlia-based P2P peer discovery using `libp2p` for total independence.
3.  **Local Mesh**: Zero-config mDNS discovery for LAN/Air-gapped communication.

### 💎 Bit-Perfect Aero UI
We have dissected the original 7.41 resources to deliver a premium, native-compiled (non-Electron) interface:
*   **Glassmorphism & Aero Effects**: Hardware-accelerated translucency.
*   **Multi-Window Multitasking**: Detachable, floating chat windows.
*   **Master Forensic Spritesheets**: Original 2014 icon sets and assets.
*   **Iconic Animated Emoticons**: The full set of legendary Skype 7 emoticons.

---

## 🚀 Deployment & Build

### 1. Build the Phaze Nexus (Relay Server)
```bash
cd nexus_server
go build -o phaze-nexus
./phaze-nexus
```

### 2. Build the Phaze Client (Native Desktop)
```bash
cd native_client
go build -o phaze
./phaze
```

### 📹 Sovereign Media (CoTURN Integration)
For high-fidelity video calling across restrictive firewalls:
1.  **Configure CoTURN**: Use the provided `scripts/phaze_turnserver.conf`. Generate a fresh `static-auth-secret` per deployment with `openssl rand -hex 32`.
2.  **Set Environment on the Nexus server** (same value as `static-auth-secret`):
    *   `PHAZE_TURN_SECRET`: The HMAC-SHA1 secret.
    *   `PHAZE_TURN_URL`: The TURN server address (e.g. `turn:turn.phazechat.world:3478`).
    *   `PHAZE_TURN_SHORT_TERM`: `true` for 10-min creds, otherwise 24h.

---

## 🛠️ Technical Specification

*   **Signaling**: Proprietary Phaze-Mesh WebSocket protocol with automatic supernode failover.
*   **Cryptography**: End-to-End Encryption using `x/crypto/nacl/box` (Curve25519, XSalsa20, Poly1305).
*   **UI Framework**: Fyne v2.5+ (Native OpenGL/Vulkan/Metal backend) for hardware-accelerated Aero effects.
*   **Persistence**: SQLite 3.45+ with forensic parity for the classic `main.db` schema.
*   **Media**: Native WebRTC integration with sovereign CoTURN orchestration.

---

## 🤝 The Forensic Mesh
Phaze is built by sovereign engineers for sovereign users. This is an open-source movement to reclaim the communication technology of the past to secure the freedom of the future.

---
*Phaze is a bit-perfect reconstruction and is not affiliated with, endorsed by, or associated with Microsoft Corporation or the Skype brand.*
