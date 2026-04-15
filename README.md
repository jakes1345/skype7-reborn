# Skype 7 — Reborn in Go

A pixel-faithful remake of Skype 7 written entirely in Go. No Electron, no
cgo, no Microsoft servers — just a native desktop client talking to a
self-hostable relay.

![status](https://img.shields.io/badge/status-alpha-orange)
![go](https://img.shields.io/badge/go-1.25%2B-00ADD8)
![platform](https://img.shields.io/badge/platform-linux%20%7C%20macOS%20%7C%20windows-lightgrey)

## What's in the box

- **`native_client/`** — Fyne desktop app. Classic Skype 7 Aero UI,
  emoticon parser, contacts, group chats, 1:1 and group voice calls,
  file transfer, presence/mood, typing indicators, read receipts.
- **`nexus_server/`** — Pure-Go WebSocket relay. bcrypt auth, SQLite
  storage, friend requests, conversations, offline message delivery,
  WebRTC signaling.

Voice uses WebRTC + PCMU (G.711 µ-law) @ 8 kHz with PulseAudio I/O on
Linux. Everything is pure Go — no libopus, no libwebrtc, no cgo.

## Quick start

```bash
# 1. Run the relay
go run ./nexus_server

# 2. Run the client (in another terminal)
go run ./native_client
```

First launch prompts you to register. Passwords are bcrypt-hashed on
the server and stored in the OS keyring on the client.

## Self-hosting the relay

```bash
go build -o nexus ./nexus_server
./nexus -addr :8443 -db nexus.db
```

Point clients at it by editing the server URL in Settings → Advanced.

## Downloads

Pre-built binaries for Linux, macOS and Windows are attached to each
[GitHub Release](../../releases). Grab the archive for your platform,
unzip, run.

## Build from source

Requires Go 1.25+.

```bash
go build -o skype        ./native_client
go build -o skype-nexus  ./nexus_server
```

Cross-compile:

```bash
GOOS=windows GOARCH=amd64 go build -o skype.exe ./native_client
```

## Assets

`native_client/assets/` contains original Skype 7 sounds, the Tahoma
font, and the UI sprite sheet used for presence dots, call buttons,
and emoticons. These are extracted from the preserved installers in
`research_files/`.

## License

MIT. See [LICENSE](LICENSE). Skype is a trademark of Microsoft — this
project is an independent, non-commercial homage.
