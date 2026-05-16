# Phaze™ build system

.PHONY: all desktop android ios iossim nexus clean help test vet verify web-build build-all phaze-assets

APP_NAME=Phaze
PACKAGE_ID=world.phazechat.app
VERSION=1.0.0
# Android SDK (default). Override with env or copy local.mk.example → local.mk
ANDROID_HOME ?= $(HOME)/Android/Sdk
-include local.mk

all: help

build-all: ## Full stack: tests → nexus → desktop → web → Android APK (bin/ + web/dist/)
	@echo "═══════════════════════════════════════════════════════════════════"
	@echo " Phaze build-all: test → nexus → desktop → web-build → android"
	@echo "═══════════════════════════════════════════════════════════════════"
	@$(MAKE) test nexus desktop web-build android
	@echo ""
	@echo "[Phaze] build-all done. Artifacts:"
	@ls -la bin/ 2>/dev/null || true
	@echo "  web/dist/  (static web client)"

web-build: ## Production build of web/ (requires npm)
	@echo "[Phaze] Building web client..."
	cd web && npm ci && npm run build

desktop: ## Build native desktop binaries for current OS
	@echo "[Phaze] Building native desktop binary..."
	@mkdir -p bin
	cd native_client && go build -o ../bin/$(APP_NAME) .

windows: ## Cross-compile for Windows
	@echo "[Phaze] Building Windows binary (x64)..."
	cd native_client && fyne-cross windows --arch amd64 -app-id $(PACKAGE_ID)

linux: ## Cross-compile for Linux
	@echo "[Phaze] Building Linux binary (x64)..."
	cd native_client && fyne-cross linux --arch amd64 -app-id $(PACKAGE_ID)

darwin: ## Cross-compile for macOS
	@echo "[Phaze] Building macOS binary (Universal)..."
	cd native_client && fyne-cross darwin --arch amd64,arm64 -app-id $(PACKAGE_ID)

## 📱 Mobile Targets (see docs/MOBILE_BUILDS.md — need ANDROID_HOME + NDK)
android: ## Build Android arm64 APK via fyne package → bin/Phaze-android-arm64.apk
	@echo "[Phaze] Android: ANDROID_HOME=$(ANDROID_HOME)"
	@ndk=$$(ls -d "$(ANDROID_HOME)/ndk"/* 2>/dev/null | sort -V | tail -1); \
	if [ -z "$$ndk" ] && [ -d "$(ANDROID_HOME)/ndk-bundle" ]; then ndk="$(ANDROID_HOME)/ndk-bundle"; fi; \
	if [ -z "$$ndk" ]; then echo "Install Android NDK: Android Studio → SDK Manager → NDK, or sdkmanager. See docs/MOBILE_BUILDS.md"; exit 1; fi; \
	mkdir -p bin && cd native_client && \
	ANDROID_HOME="$(ANDROID_HOME)" ANDROID_NDK_HOME="$$ndk" fyne package -os android/arm64 --src . --id $(PACKAGE_ID) --name $(APP_NAME) && \
	apk=$$(ls *.apk 2>/dev/null | head -1); \
	if [ -z "$$apk" ]; then echo "fyne package did not produce an .apk in native_client/"; exit 1; fi; \
	mv "$$apk" ../bin/Phaze-android-arm64.apk && echo "[Phaze] APK → bin/Phaze-android-arm64.apk"

ios: ## iOS / simulator build on macOS only (fyne package)
	@echo "[Phaze] iOS packaging (requires macOS + Xcode)..."
	@if [ "$$(uname -s)" != "Darwin" ]; then echo "Run on macOS with Xcode. See docs/MOBILE_BUILDS.md"; exit 1; fi
	@mkdir -p bin && cd native_client && \
	fyne package -os ios --src . --id $(PACKAGE_ID) --name $(APP_NAME) && \
	echo "[Phaze] iOS artifact produced under native_client/ (see fyne output for .ipa / .app)"

iossim: ## iOS Simulator build on macOS (often friendlier for CI than device signing)
	@echo "[Phaze] iOS Simulator packaging..."
	@if [ "$$(uname -s)" != "Darwin" ]; then echo "Run on macOS with Xcode."; exit 1; fi
	@mkdir -p bin && cd native_client && \
	fyne package -os iossimulator --src . --id $(PACKAGE_ID) --name $(APP_NAME)

## 🌐 Server Targets
nexus: ## Build the Nexus Relay Server
	@echo "[Phaze] Building Nexus Relay Server..."
	@mkdir -p bin
	cd nexus_server && go build -o ../bin/phaze-nexus .

## 🛡️ Maintenance
phaze-assets: ## Regenerate WAVs, PNG emoticons, branding, spritesheet → Nexus public + native_client/assets/
	@echo "[Phaze] Generating sounds → nexus_server/public/phaze/assets/sounds ..."
	@mkdir -p "nexus_server/public/phaze/assets/sounds"
	cd native_client && go run ./cmd/soundgen "../nexus_server/public/phaze/assets/sounds"
	@echo "[Phaze] Generating emoticons + branding → nexus_server/public/phaze/assets ..."
	@mkdir -p "nexus_server/public/phaze/assets/emoticons"
	cd native_client && go run ./cmd/emoticongen "../nexus_server/public/phaze/assets"
	@echo "[Phaze] Nexus default avatar path (ServeFile) → nexus_server/assets/ ..."
	@mkdir -p nexus_server/assets
	cp -f nexus_server/public/phaze/assets/default_avatar.png nexus_server/assets/default_avatar.png 2>/dev/null || true
	@echo "[Phaze] Copying phaze assets → native_client/assets/ (gitignored; local fyne/desktop) ..."
	@mkdir -p native_client/assets/sounds native_client/assets/emoticons
	cp -f nexus_server/public/phaze/assets/sounds/*.wav native_client/assets/sounds/ 2>/dev/null || true
	cp -f nexus_server/public/phaze/assets/emoticons/*.png native_client/assets/emoticons/ 2>/dev/null || true
	cp -f nexus_server/public/phaze/assets/Icon.png nexus_server/public/phaze/assets/phaze_logo.png nexus_server/public/phaze/assets/default_avatar.png nexus_server/public/phaze/assets/ui_master_spritesheet.png native_client/assets/ 2>/dev/null || true

test: ## Run Go tests (nexus + native, all packages)
	@echo "[Phaze] Running tests..."
	cd nexus_server && go test ./...
	cd native_client && go test ./...

vet: ## go vet on nexus_server and native_client
	@echo "[Phaze] go vet..."
	cd nexus_server && go vet ./...
	cd native_client && go vet ./...

verify: test vet ## tests + vet + web build + ESLint (needs npm)
	@echo "[Phaze] Web verify..."
	cd web && npm ci && npm run build && npm run test && npm run lint

clean: ## Purge all build artifacts
	@echo "[Phaze] Purging artifacts..."
	rm -rf bin/ fyne-cross/

help: ## Show this help menu
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
