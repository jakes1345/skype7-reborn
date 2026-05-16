# Linux distribution options (Flatpak, packages, raw binary)

Phaze ships **generic Linux archives** on [GitHub Releases](https://github.com/jakes1345/skype7-reborn/releases) (see `Phaze_linux_amd64.tar.gz` / `Phaze_linux_arm64.tar.gz` once your release pipeline publishes them). Those bundles include the desktop client plus the `phaze-nexus` relay binary and supporting files from GoReleaser.

## Flatpak / Flathub

**Flatpak** is the closest thing Linux has to a cross‑distro “app store” with automatic updates (`flatpak update`). **Flathub** is the main public repository.

Publishing on Flathub is a **separate submission** from this repo: you add a Flatpak manifest (usually `io.github.USER.REPO.yml`), AppStream metadata (`.metainfo.xml`), desktop entry, icons, and open a PR against [flathub/flathub](https://github.com/flathub/flathub). Builds run on Flathub’s infrastructure.

This repository does **not** yet include a Flathub manifest; treat Flatpak as a **follow‑up** if you want discoverability and background updates. Until then, users should install from GitHub or distro packages.

## Other update paths

| Approach | Updates |
|----------|---------|
| **GitHub Releases** | Manual re‑download or a small script watching the API |
| **AppImage** + [AppImageUpdate](https://appimage.github.io/) | Zsync / embedded update info (optional future work) |
| **Distro packages** (DEB/RPM/AUR) | Distribution’s package manager |

## Verifying downloads

Use `checksums.txt` attached to each GitHub release and compare with `sha256sum` on the downloaded archive.
