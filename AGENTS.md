## Learned User Preferences

- When exploring Skype-related Windows binaries on Linux, prefers thorough automated analysis (for example Ghidra headless batch exports and radare2) rather than strings-only inspection.

## Learned Workspace Facts

- Product code lives under `nexus_server/`, `native_client/`, `web/`, `docs/`, and `scripts/`. Clients (web + Fyne desktop/Android) should target the **same Nexus** `NexusMessage` protocol; see `docs/CLIENT_INTEROP.md` for parity gaps (notably web WebRTC/codec alignment for calls).
