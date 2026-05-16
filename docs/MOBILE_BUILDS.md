# Phaze mobile & desktop packaging (Android / iOS / macOS)

Phaze **`native_client`** is a **Fyne** app. There is **no separate Android Studio Gradle project** in this repo — Fyne generates the Android/iOS glue. You still use **Android Studio** (or standalone **cmdline-tools**) to install **SDK + NDK + platforms** on your machine.

Canonical app ID: **`world.phazechat.app`** (see `native_client/FyneApp.toml`). Use the same ID everywhere (Android package name, iOS bundle id, `make` `PACKAGE_ID`).

---

## 1. Android Studio on Linux (`.tar.gz` IDE vs SDK)

If you unpacked the IDE to something like **`/home/jack/android-studio`**, that directory is **only Android Studio** (binaries, JBR, plugins). It is **not** the same as **`ANDROID_HOME`**.

After you run **`bin/studio.sh`** once and open **SDK Manager**, the Android SDK almost always ends up under:

| Role | Example path on this machine |
|------|-------------------------------|
| **IDE** | `/home/jack/android-studio` |
| **SDK (`ANDROID_HOME`)** | `/home/jack/Android/Sdk` |

Set `ANDROID_HOME` to the **Sdk** folder, not the `android-studio` folder. In this repo you can copy **`local.mk.example`** → **`local.mk`** (gitignored) so `make android` picks it up automatically.

---

## 1b. Default install paths (reference)

| Piece | Typical path |
|--------|----------------|
| **SDK root** | `$HOME/Android/Sdk` |
| **Platform** | `$HOME/Android/Sdk/platforms/android-34` (or similar) |
| **NDK** | `$HOME/Android/Sdk/ndk/<version>` (required for Fyne Android builds) |
| **sdkmanager** | `$HOME/Android/Sdk/cmdline-tools/latest/bin/sdkmanager` |

Set for builds:

```bash
export ANDROID_HOME="$HOME/Android/Sdk"
# After NDK is installed, either:
export ANDROID_NDK_HOME="$HOME/Android/Sdk/ndk/$(ls "$HOME/Android/Sdk/ndk" | sort -V | tail -1)"
# or let `make android` pick the newest folder under `$ANDROID_HOME/ndk/`
```

**Install NDK** (pick one):

- **Android Studio** → *Settings* → *Languages & Frameworks* → *Android SDK* → *SDK Tools* → check **NDK (Side by side)** → Apply.
- **Command line** (no Studio `sdkmanager` yet): from the repo root run **`scripts/install-android-ndk.sh`** (installs cmdline-tools + NDK 27 under `$ANDROID_HOME` or `~/Android/Sdk`).
- **Command line** (if `sdkmanager` already exists):

  ```bash
  yes | "$HOME/Android/Sdk/cmdline-tools/latest/bin/sdkmanager" --sdk_root="$HOME/Android/Sdk" "ndk;27.0.12077973" "platforms;android-34" "build-tools;34.0.0"
  ```

Without an NDK directory, `fyne package -os android/...` fails with **“no Android NDK found”**.

### NDK 27+ and `ALooper_pollAll`

Google removed **`ALooper_pollAll`** from the NDK C headers (use **`ALooper_pollOnce`**). This repo vendors `golang.org/x/mobile` under `native_client/third_party/mobile_x` with a one-line fix so Fyne Android builds link against current NDKs.

---

## 2. Recommended CLI: `fyne package` (not legacy `fyne-cross`)

The old **`fyne-cross`** fork is **deprecated**; upstream points to:

```bash
go install fyne.io/tools/cmd/fyne@latest
```

This repo’s **`Makefile`** uses **`fyne package`** for **Android** and **iOS** (on macOS). Cross-compiling **desktop** Linux/Windows/macOS from one OS may still use **`fyne-cross`** where Fyne does not support remote targets from your host — see `make linux` / `windows` / `darwin`.

---

## 3. One-shot full client + relay + web (`make build-all`)

From repo root:

```bash
make build-all
```

Runs **`make test`**, then builds **`bin/phaze-nexus`**, **`bin/Phaze`** (desktop for this OS), **`web/dist/`**, and **`bin/Phaze-android-arm64.apk`**. Requires **`ANDROID_HOME`** + NDK (see §1).

iOS is **not** part of `build-all` (macOS + Xcode only); use **`make ios`** or **`make iossim`** on a Mac.

### Android APK only

```bash
export ANDROID_HOME="$HOME/Android/Sdk"
make android
```

Produces **`bin/Phaze-android-arm64.apk`**.

Manual `fyne package` (same as `make android`):

```bash
cd native_client
export ANDROID_HOME="$HOME/Android/Sdk"
export ANDROID_NDK_HOME="$HOME/Android/Sdk/ndk/$(ls "$ANDROID_HOME/ndk" | sort -V | tail -1)"
fyne package -os android/arm64 --src . --id world.phazechat.app --name Phaze
```

---

## 4. macOS desktop app

On a **Mac** (Apple Silicon or Intel), or **only via GitHub**: this repo’s workflow **[Sovereign Apple Verification](https://github.com/jakes1345/skype7-reborn/actions)** runs **`go build -o Phaze .`** on **`macos-latest`** on every push/PR to **`main` / `master`** — that’s the “everything is linked to GitHub” path when you don’t have a physical Mac for smoke builds.

```bash
cd native_client
go build -o Phaze .
# or create a .app bundle:
go install fyne.io/tools/cmd/fyne@latest
fyne package -os darwin --src . --id world.phazechat.app --name Phaze
```

Workflow: `.github/workflows/apple-verify.yml` (same `go build` as above; full module, not `main.go` alone).

---

## 5. iOS (IPA / simulator)

**Requirements:** **macOS + Xcode** (Command Line Tools + full Xcode for device builds). Apple **signing** (team, certificate, provisioning profile) is required for **device** IPAs; for **CI smoke** without a paid profile, prefer **iOS Simulator** when supported — the repo runs **`fyne package -os iossimulator`** on **`macos-latest`** in **[Actions](https://github.com/jakes1345/skype7-reborn/actions)** (workflow **Sovereign Apple Verification**).

```bash
cd native_client
fyne package -os iossimulator --src . --id world.phazechat.app --name Phaze
```

For **release on device**, use your Apple Developer account and Fyne’s signing flags (`--certificate`, `--profile`) or Xcode after export — see [Fyne mobile packaging](https://developer.fyne.io/started/mobile).

---

## 6. How this links to Nexus / phazechat.world

Mobile and desktop share the same **`PhazeInfra`** defaults in `native_client/main.go` (`https://phazechat.world`, `wss://phazechat.world/ws`). After you ship an APK/IPA, users still need your **Nexus relay** reachable at that host (see **`docs/DEPLOY_SELF_HOSTED.md`**). No extra “linking” step in code beyond TLS + DNS.

---

## 7. Troubleshooting

| Symptom | Fix |
|---------|-----|
| `no Android NDK found` | Install NDK under `$ANDROID_HOME/ndk/` or set `ANDROID_NDK_HOME`. |
| `ANDROID_HOME` empty | Export it or install Android Studio / SDK. |
| iOS build fails on Linux | Expected — build iOS **on macOS** only. |
| `go build` missing symbols | Build **package** with `go build .` from `native_client`, not `go build main.go` alone. |
