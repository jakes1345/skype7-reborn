#!/usr/bin/env bash
# Install Android SDK command-line tools + NDK into ANDROID_HOME (default: ~/Android/Sdk).
# Run once on Linux if `make android` says NDK is missing and you have no sdkmanager yet.
set -euo pipefail
SDK="${ANDROID_HOME:-$HOME/Android/Sdk}"
ZIP="${TMPDIR:-/tmp}/commandlinetools-linux-11076708_latest.zip"
URL="https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip"

echo "SDK root: $SDK"
mkdir -p "$SDK"
if [[ ! -f "$ZIP" ]]; then
  curl -fsSL -o "$ZIP" "$URL"
fi
rm -rf /tmp/ct-unpack-android-ndk
mkdir -p /tmp/ct-unpack-android-ndk
unzip -qo "$ZIP" -d /tmp/ct-unpack-android-ndk
mkdir -p "$SDK/cmdline-tools/latest"
rm -rf "$SDK/cmdline-tools/latest"/*
mv /tmp/ct-unpack-android-ndk/cmdline-tools/* "$SDK/cmdline-tools/latest/"
SM="$SDK/cmdline-tools/latest/bin/sdkmanager"
yes | "$SM" --sdk_root="$SDK" --licenses >/tmp/sdk-licenses.log 2>&1 || true
"$SM" --sdk_root="$SDK" "ndk;27.0.12077973"
echo "Done. NDK:" && ls "$SDK/ndk"
