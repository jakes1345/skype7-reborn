# Phaze — legal / clean-room posture

Phaze is an **independent** chat and calling stack (Go relay + clients). It does **not** ship Microsoft Skype binaries, decompiled source, or ripped Skype assets in this repository.

## 1. Original implementation

- Application logic is written in **Go** (and TypeScript for the web client) against the **Phaze Nexus** wire protocol documented in `docs/WS_PROTOCOL.md`.
- **No** incorporation of decompiled Skype code or copy-pasted proprietary logic is intended in product paths under `nexus_server/`, `native_client/`, and `web/`.

## 2. Assets and branding

- **Branding** is Phaze-specific. Do not commit Microsoft-owned logos, sounds, or artwork.
- **Icons and sounds** used in builds should be originals, licensed stock, or user-supplied files kept **out of git** when copyright is unclear (see `.gitignore` for `native_client/assets/`).

## 3. Disclaimers

This project is not affiliated with Microsoft or Skype. If you perform interoperability research on your own machine, comply with applicable law and your own counsel’s guidance; this file is not legal advice.
