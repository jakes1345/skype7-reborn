# Voice and video: WebRTC first, PSTN optional

Phaze is built around **WebRTC** for calls between users:

| Surface | Stack | Signaling |
|---------|--------|-----------|
| **Desktop / Android** | [Pion WebRTC v3](https://github.com/pion/webrtc) (`native_client/internal/chat/webrtc.go`) | Nexus WebSocket: `call_offer`, `call_answer`, `ice_candidate`, `call_reject`, `call_end` (SDP/ICE payloads can be E2EE-wrapped like DMs) |
| **Web** | Browser `RTCPeerConnection` (planned / in progress in `web/`) | Same Nexus message types |
| **TURN** | CoTURN (or env fallbacks) | `turn_config` on `auth_result` / `session_auth` from Nexus |

Media is **peer-to-peer** when NAT allows; otherwise **TURN** relays encrypted RTP (relay operator sees ciphertext on the wire for SRTP, but not application-layer content from Phaze’s NaCl-wrapped signaling bodies where used).

## Why skip PSTN by default

**PSTN** (calling real phone numbers) uses **Twilio** (or similar) and costs money per minute. Phaze does not need it for core product value: **Phaze-to-Phaze voice/video is free** aside from your own server and TURN bandwidth.

## Enabling PSTN (optional)

If you still want outbound phone bridge:

1. Set Twilio env vars on Nexus (`TWILIO_SID`, `TWILIO_TOKEN`, `TWILIO_FROM`, `Phaze_APP_URL`) as today.
2. Set **`PHAZE_ENABLE_PSTN=true`** on the **Nexus** process. Without this, `pstn_call` is rejected with a clear message and no Twilio call is attempted.
3. Set **`PHAZE_ENABLE_PSTN=true`** on **desktop** to show the numeric **Dial** tab again (same env name for consistency).

Phone **verification** (`request_phone_link` / SMS) is separate from PSTN calls and still uses Twilio SMS when those credentials are present.

## Product direction

Prefer **in-chat WebRTC** (phone/camera buttons in a conversation) for all user-to-user audio/video. Reserve PSTN for a future “call a phone number” premium feature, or remove it entirely once you confirm you do not need it.
