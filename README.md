# TAZHER™ 7.41 REBORN
### Communication Sovereignty for a Post-Centralized World

![TAZHER Hero](https://github.com/jakes1345/skype7-reborn/blob/master/assets/tazher_hero_mesh.png?raw=true)

**TAZHER™** is a sovereign, production-grade forensic reconstruction of the classic 7.41 communication experience. It is not just a remake; it is an unstoppable communication engine designed for absolute privacy, resilience, and high-fidelity nostalgia.

---

## ⚡ Key Pillars

### 🛡️ The Sovereign Shield (E2EE)
TAZHER utilizes **End-to-End Encryption (NaCl/Box)** as its native standard. Every message is cryptographically sealed at the source. The Nexus Relay remains **Zero-Knowledge**, never seeing anything but encrypted cipher-blobs.

### 🧠 The Sentinel Internal AI
Embedded within the core is the **TAZHER Sentinel**, an autonomous self-healing agent that monitors connection heartbeats across the mesh. If a relay is seized or the network fragments, the Sentinel autonomously repairs the link or fails over to the P2P DHT.

### 🌐 The Three-Layer Mesh
Communication is guaranteed through three redundant signaling layers:
1.  **Nexus Cloud**: High-speed global relays (Fly.io enabled).
2.  **Sovereign DHT**: Kademlia-based P2P peer discovery using `libp2p`.
3.  **Local Mesh**: Zero-config mDNS discovery for LAN/Air-gapped communication.

### 💎 Bit-Perfect Aero UI
Experience the legendary 2014 aesthetic in its full glory. TAZHER delivers a premium, native-compiled (non-Electron) interface featuring:
*   **Glassmorphism & Aero Effects**
*   **Multi-Window Multitasking** (Detachable Chat Windows)
*   **Master Forensic Spritesheets**
*   **Iconic Animated Emoticons**

### 📹 Sovereign Media (CoTURN)
For unstoppable video calling across restrictive firewalls:
1.  **Install CoTURN**: `sudo apt install coturn`
2.  **Configure**: `sudo cp scripts/tazher_turnserver.conf /etc/turnserver.conf`
3.  **Deploy**: `sudo systemctl restart coturn`
4.  **Connect**: Set `TAZHER_TURN_SECRET` and `TAZHER_TURN_URL` on your Nexus server.

---

## 🚀 Quick Start (Development)

### 1. Build the Nexus Relay
```bash
cd nexus_server
go build -o tazher-nexus
./tazher-nexus
```

### 2. Build the Native Client
```bash
cd native_client
go build -o tazher-client
./tazher-client
```

---

## 🏗️ Technical Architecture

*   **Logic**: Go 1.25.0
*   **UI Framework**: Fyne v2 (Native Backend)
*   **P2P Library**: LibP2P (Kademlia DHT + mDNS)
*   **Encryption**: NaCl/SecretBox (XSalsa20 + Poly1305)
*   **Database**: SQLite (Forensic 7.41 Schema Parity)

---

## 🤝 Contributing
TAZHER is built by sovereign engineers. If you are a forensic developer or a UI architect, join the mesh.

---
*TAZHER is a bit-perfect reconstruction and is not affiliated with Microsoft Corporation.*
