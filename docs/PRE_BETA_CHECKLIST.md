# Pre-beta finalization checklist

Use this before inviting **public** beta testers. Check boxes as you complete items; link evidence (PR, run log, issue) in your release notes where useful.

**Related:** [BETA.md](BETA.md) (ship-day gate), [README.md](../README.md) (threat model + known gaps).

---

## 1. Build, CI, and release

- [ ] `master` (or release branch) has **green** `.github/workflows/ci.yml` on Linux.
- [ ] `.github/workflows/release.yml` Linux image matches CI deps (VP8 / Fyne stack).
- [ ] `apple-verify.yml` Go version **matches** `go.work` / production intent (avoid silent drift).
- [ ] **iOS / IPA**: not produced in GitHub Actions (fyne-cross needs Docker); build on a Mac with Docker or your release pipeline. `verify-ios` in CI is an explicit documentation step only.
- [ ] Goreleaser dry-run locally or on tag: **both** `phaze` and `phaze-nexus` artifacts expected.
- [ ] **Version strings**: `Phaze_LATEST_VERSION` (or equivalent) documented for operators.
- [ ] **Checksums** published with release; verify one download end-to-end.

---

## 2. Security and abuse

- [ ] **CORS**: `Phaze_ALLOWED_ORIGINS` set in production (not “allow all”).
- [ ] **Secrets**: `PHAZE_TURN_SECRET`, DB path, SMTP/Twilio keys — no defaults in prod; not in logs.
- [ ] **Rate limits**: registration, login, password reset, WS message flood — spot-check under load.
- [ ] **Session / TOTP**: lockout or backoff on repeated failures; QR login token single-use and expiry.
- [ ] **SQL**: no dynamic string-built SQL on hot paths; parameters everywhere.
- [ ] **E2EE coverage audit**: any new `NexusMessage` fields that must be client-encrypted are documented and wrapped.
- [ ] **TOFU / key mismatch**: user-visible error when pin rejects (not only server/client logs).
- [ ] **Asset vault** (compiled key): disclosed in beta notes — testers understand limitation.

---

## 3. Relay and real-time

- [ ] **`GET /health`**: returns `200` with `database_ok: true` and sensible `turn_configured` in staging/prod.
- [ ] **TURN**: CoTURN `static-auth-secret` matches relay env; short-term mode matches README.
- [ ] **WebSocket**: TLS termination (if any) does not strip auth; idle timeouts acceptable.
- [ ] **Offline queue**: spot-check offline delivery + no duplicate storms after reconnect.

---

## 4. New-user and UX

- [ ] **Cold path**: install → first launch → register → verify email → first message (happy path recorded).
- [ ] **Error copy**: network failure, wrong password, unverified account — each says what to do next.
- [ ] **Empty states**: no contacts / no chats — still navigable.
- [ ] **Windows** video/audio caveats mentioned where users look (README + download page).
- [ ] **Android ↔ desktop** group chat: either verified or explicitly “not supported in this beta” in release notes.

---

## 5. Cross-platform smoke matrix

Minimum devices (adjust to your pool):

| Flow | Linux | macOS | Windows | Android |
|------|-------|-------|---------|---------|
| Register + login | | | | |
| 1:1 E2EE message | | | | |
| Voice call | | | | |
| Group message | | | | |
| File send (small) | | | | |

- [ ] Symmetric NAT / tether call at least once.
- [ ] **VPN** or double-NAT spot-check (expect TURN-heavy).

---

## 6. Data, privacy, support

- [ ] **Privacy / terms** pages match actual behavior (email stored, IPs in logs, retention).
- [ ] **Support path**: GitHub Issues + template; response expectation (best-effort / SLA) stated in release.
- [ ] **Backup**: self-hosters know how to back up `nexus.db` (and restore drill once).

---

## 7. Observability and incidents

- [ ] **Logs**: where they go in prod; log rotation; no password/token lines in debug paths.
- [ ] **Alerts** (minimal): disk full, process restart, 5xx rate — even manual is fine for beta.
- [ ] **Runbook** one-pager: “TURN broken”, “DB locked”, “spam registration wave”.

---

## 8. Documentation truth

- [ ] [docs/index.html](index.html) aligned with README (Phaze naming, features, binary name).
- [ ] Download / marketing templates do not promise **Opus**, **federation**, or **installers** unless shipped.
- [ ] `docs/BETA.md` linked from README.

---

## 9. Legal and brand

- [ ] “Not Microsoft / not Skype” disclaimer visible in app and site footers.
- [ ] Binary and window titles consistent (**Phaze** vs legacy strings).

---

## 10. Post-checkpoint

- [ ] Tag beta (e.g. `beta/v0.9.0-rc1`) and publish **draft** release for maintainer review.
- [ ] After 24h soak on staging, flip to **public** prerelease and announce.

When this file is complete enough for your risk tolerance, you are ready to execute [BETA.md](BETA.md).
