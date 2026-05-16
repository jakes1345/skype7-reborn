# Phaze public beta — scope and gate checklist

This document is the **operational definition** of “beta” for Phaze (Skype 7 Reborn lineage): what must be true before you invite strangers, and what is explicitly out of scope for the first beta wave.

Complete **[docs/PRE_BETA_CHECKLIST.md](PRE_BETA_CHECKLIST.md)** first (security, matrix testing, doc truth), then execute the numbered gates below.

## What “beta” means here

- **Product**: Pre-1.0. Core flows are implemented; **field verification** on real networks and devices is the main remaining risk (see `README.md` status).
- **Goal of the beta**: Collect **crash reports**, **relay edge cases**, and **cross-platform call/chat** feedback—not feature parity with legacy Skype.

## Same-day beta checklist (maintainer / release captain)

1. **Green CI on `master`**  
   Linux CI must install **libvpx** (and the usual Fyne/X11/Pulse deps) so `native_client` actually compiles; see `.github/workflows/ci.yml`.

2. **Tag a prerelease**  
   Example: `git tag beta/v0.9.0-rc1 && git push origin beta/v0.9.0-rc1`  
   The Release workflow matches tags `v*` and `beta*` (see `.github/workflows/release.yml`).

3. **Smoke matrix (minimum)**  
   - Register → sign in → add contact → 1:1 message (E2EE path).  
   - Voice call (expect **PCMU** quality; Opus is post-beta roadmap).  
   - One **group chat** send on **desktop ↔ desktop**.  
   - **Android ↔ desktop** group path: README calls this **untested**; treat result as either “beta unblocked” or “document known-good combos”.

4. **Relay + TURN**  
   Confirm production `PHAZE_TURN_*` env matches CoTURN; calls fail closed without working TURN on many NATs.

5. **Support surface**  
   Single place for reports (GitHub Issues + optional Discord/Matrix). Set expectations: **no installers** yet (archives only unless you add them in this release).

## Known limitations (do not treat as “bugs” for beta exit)

From `README.md` (non-exhaustive): **Opus not shipped**; **no federation**; **asset vault key compiled in**; **no auto-update**; **Windows video path** still weaker than Linux/macOS VP8; **no structured relay metrics** (only a minimal `/health` JSON today).

## Post-beta roadmap themes (not same-day)

- Opus or wideband audio path.  
- Installers + update channel.  
- Relay observability (metrics, structured logs).  
- Group forward-secrecy / crypto hardening.  
- Optional **web client** (greenfield on Phaze APIs; not a decompile of Skype).

## Stale docs

- [x] `docs/index.html` updated to match README (Phaze, current feature set, `phaze` binary name).
- Full narrative site lives under `nexus_server/templates/` for production pages.
