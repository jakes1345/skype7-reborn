# TAZHER: Sovereign Communication Engine (Skype 7 Resurrection)

TAZHER is a high-performance, native-compiled reconstruction of the classic Skype 7.x experience. It is designed for **Sovereignty**, meaning it works regardless of whether central servers are online or offline.

## 🚀 The TAZHER Promise
- **Native Go Core**: No Electron, no WINE. Light on RAM, fast on CPU.
*   **Unified Mesh**: Combines a fast Nexus Cloud with a resilient P2P DHT and LAN mDNS discovery. You are never "offline."
*   **Obsidian Aesthetics**: Premium glassmorphism UI that respects the classic Aero layout.
*   **Bit-Perfect Audio**: Authentic wav triggers for every event.

## 🛠 Features
| Feature | Status | Notes |
| :--- | :--- | :--- |
| **Obsidian UI** | ✅ Ready | Glassmorphism, Aero-inspired sidebar. |
| **Unified Mesh** | ✅ Ready | Parallel dialer (Local + Cloud + P2P). |
| **P2P Discovery** | ✅ Ready | DHT + LAN (mDNS) Hood Protocol. |
| **Messaging** | ✅ Ready | Real-time chat with "Skype-Style" history. |
| **Voice Calls** | 🚀 Ready | WebRTC with **Obsidian Call Overlay** and Pulse animations. |
| **Security** | ✅ Ready | Email-backed Auth to prevent abuse. |
| **Updates** | ✅ Ready | Global cloud-sync regardless of connection mode. |

## 🚀 Getting Started

TAZHER is a "Ship-it-all" package. 

### 1. Requirements
- **Go 1.25+** (Required for modern libp2p networking).

### 2. Launch the Unified Mesh
To experience the full Tazher network locally:
```bash
# Run the orchestrator
./run_local_tazher.sh
```
This will launch your own **Local Nexus Node** and the **Tazher Client**, which will connect to both your local node and the global mesh simultaneously.

## 🏗 Architecture
TAZHER operates as a **Three-Layer Mesh**:
1. **Layer 1: Nexus Cloud**: High-speed global relays for instant connectivity.
2. **Layer 2: Local Nexus**: Personal/LAN nodes for private work centers.
3. **Layer 3: P2P DHT**: The "Unstoppable" back-channel that finds peers even when all servers are seized.

---
*TAZHER is an independent preservation and communication project. "Don't stop til you've had enough."*

