# Phaze™ 7.41 Sovereign Build System
# (c) 2024-2026 Phaze. Bit-Perfect Forensic Reconstruction.

.PHONY: all desktop android nexus clean help test

APP_NAME=Phaze
PACKAGE_ID=world.phazechat.app
VERSION=1.0.0

all: help

## 💻 Desktop Targets
desktop: ## Build native desktop binaries for current OS
	@echo "[Phaze] Building native desktop binary..."
	cd native_client && go build -o ../bin/$(APP_NAME) main.go

windows: ## Cross-compile for Windows
	@echo "[Phaze] Building Windows binary (x64)..."
	cd native_client && fyne-cross windows --arch amd64 -app-id $(PACKAGE_ID)

linux: ## Cross-compile for Linux
	@echo "[Phaze] Building Linux binary (x64)..."
	cd native_client && fyne-cross linux --arch amd64 -app-id $(PACKAGE_ID)

darwin: ## Cross-compile for macOS
	@echo "[Phaze] Building macOS binary (Universal)..."
	cd native_client && fyne-cross darwin --arch amd64,arm64 -app-id $(PACKAGE_ID)

## 📱 Mobile Targets
android: ## Build native Android APK (Patched Sovereign Bridge)
	@echo "[Phaze] Executing Sovereign Android Pipeline..."
	cd native_client && fyne-cross android --arch arm64 -app-id $(PACKAGE_ID) -icon assets/Icon.png

ios: ## Build native iOS IPA (Metal Backend)
	@echo "[Phaze] Building native iOS IPA..."
	cd native_client && fyne-cross ios -app-id $(PACKAGE_ID)

## 🌐 Server Targets
nexus: ## Build the Nexus Relay Server
	@echo "[Phaze] Building Nexus Relay Server..."
	cd nexus_server && go build -o ../bin/phaze-nexus main.go

## 🛡️ Maintenance
test: ## Run the sovereign validation suite
	@echo "[Phaze] Running forensic validation tests..."
	go test ./...

clean: ## Purge all build artifacts
	@echo "[Phaze] Purging artifacts..."
	rm -rf bin/ fyne-cross/

help: ## Show this help menu
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
