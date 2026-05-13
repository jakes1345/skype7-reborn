#!/usr/bin/env bash
# Print SHA-256 lines for Nexus static mirror files (nexus_server/public/downloads/).
# Run from any cwd after updating those binaries; paste output into operator docs or status pages.
# End users should prefer GitHub Releases checksums.txt:
#   https://github.com/jakes1345/skype7-reborn/releases/latest/download/checksums.txt
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DL="$ROOT/nexus_server/public/downloads"
if [[ ! -d "$DL" ]]; then
  echo "error: missing directory $DL" >&2
  exit 1
fi
echo "# SHA-256 (GNU coreutils sha256sum format)"
for f in Phaze.exe Phaze.linux Phaze.apk; do
  if [[ ! -f "$DL/$f" ]]; then
    echo "# skip (missing): $f" >&2
    continue
  fi
  (cd "$DL" && sha256sum "$f")
done
echo ""
echo "# Verify with: sha256sum -c (after saving lines above to a file) or compare to checksums.txt on the matching GitHub release."
