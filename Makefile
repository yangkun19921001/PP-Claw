# ============================================================
# PP-Claw Makefile — 多平台交叉编译
# ============================================================

# 版本信息 (从 git tag / commit 自动获取)
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# 编译参数
BINARY   := pp-claw
BUILD_DIR := build
LDFLAGS  := -s -w \
  -X 'main.Version=$(VERSION)' \
  -X 'main.Commit=$(COMMIT)' \
  -X 'main.BuildTime=$(BUILD_TIME)'
GO_BUILD := CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)"

# ── 目标平台 ──────────────────────────────────────────────────
# 格式: GOOS/GOARCH[/GOARM|GOMIPS]
PLATFORMS := \
  linux/amd64 \
  linux/arm64 \
  linux/mips64le \
  android/arm64 \
  darwin/amd64 \
  darwin/arm64 \
  windows/amd64

# ── 默认目标 ──────────────────────────────────────────────────
.PHONY: all build build-all clean help

all: build

# 本机构建
build:
	$(GO_BUILD) -o $(BINARY) .
	@echo "✅ Built $(BINARY) for $$(go env GOOS)/$$(go env GOARCH)"

# 全平台构建
build-all:
	@echo "🦞 Building PP-Claw $(VERSION) for all platforms..."
	@mkdir -p $(BUILD_DIR)
	@$(MAKE) $(foreach p,$(PLATFORMS),build-target-$(subst /,-,$(p)))
	@echo ""
	@echo "✅ All builds complete! Output: $(BUILD_DIR)/"
	@ls -lh $(BUILD_DIR)/

# ── 各平台单独构建目标 ────────────────────────────────────────

# Linux AMD64
.PHONY: build-linux-amd64 build-target-linux-amd64
build-linux-amd64 build-target-linux-amd64:
	GOOS=linux GOARCH=amd64 $(GO_BUILD) -o $(BUILD_DIR)/$(BINARY)-linux-amd64 .
	@echo "  ✓ linux/amd64"

# Linux ARM64 (电视盒子、树莓派4)
.PHONY: build-linux-arm64 build-target-linux-arm64
build-linux-arm64 build-target-linux-arm64:
	GOOS=linux GOARCH=arm64 $(GO_BUILD) -o $(BUILD_DIR)/$(BINARY)-linux-arm64 .
	@echo "  ✓ linux/arm64"

# Linux MIPS64LE (高端路由器)
.PHONY: build-linux-mips64le build-target-linux-mips64le
build-linux-mips64le build-target-linux-mips64le:
	GOOS=linux GOARCH=mips64le $(GO_BUILD) -o $(BUILD_DIR)/$(BINARY)-linux-mips64le .
	@echo "  ✓ linux/mips64le"

# Android ARM64 (Android 电视盒子)
.PHONY: build-android-arm64 build-target-android-arm64
build-android-arm64 build-target-android-arm64:
	GOOS=android GOARCH=arm64 $(GO_BUILD) -o $(BUILD_DIR)/$(BINARY)-android-arm64 .
	@echo "  ✓ android/arm64"

# macOS ARM64 (Apple Silicon)
.PHONY: build-darwin-arm64 build-target-darwin-arm64
build-darwin-arm64 build-target-darwin-arm64:
	GOOS=darwin GOARCH=arm64 $(GO_BUILD) -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 .
	@echo "  ✓ darwin/arm64"

# macOS AMD64 (Intel Mac)
.PHONY: build-darwin-amd64 build-target-darwin-amd64
build-darwin-amd64 build-target-darwin-amd64:
	GOOS=darwin GOARCH=amd64 $(GO_BUILD) -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 .
	@echo "  ✓ darwin/amd64"

# Windows AMD64
.PHONY: build-windows-amd64 build-target-windows-amd64
build-windows-amd64 build-target-windows-amd64:
	GOOS=windows GOARCH=amd64 $(GO_BUILD) -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe .
	@echo "  ✓ windows/amd64"

# ── 清理 ──────────────────────────────────────────────────────
clean:
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY)
	@echo "🧹 Cleaned"

# ── Docker ────────────────────────────────────────────────────
.PHONY: docker
docker:
	docker build -t $(BINARY):$(VERSION) -t $(BINARY):latest .

# ── 帮助 ──────────────────────────────────────────────────────
help:
	@echo "PP-Claw Build System"
	@echo ""
	@echo "Usage:"
	@echo "  make build                  Build for current platform"
	@echo "  make build-all              Build for all platforms"
	@echo "  make build-linux-arm64      Build for specific platform"
	@echo "  make docker                 Build Docker image"
	@echo "  make clean                  Remove build artifacts"
	@echo ""
	@echo "Supported platforms:"
	@echo "  linux/amd64                 x86_64 servers, PCs"
	@echo "  linux/arm64                 TV boxes (Amlogic), RPi 4"
	@echo "  linux/mips64le              High-end routers"
	@echo "  android/arm64               Android TV boxes"
	@echo "  darwin/arm64                Apple Silicon Mac"
	@echo "  darwin/amd64                Intel Mac"
	@echo "  windows/amd64               Windows PC"
	@echo ""
	@echo "Note: 32-bit platforms (arm/mipsle) not supported due to sonic dependency."
	@echo ""
	@echo "Version: $(VERSION) ($(COMMIT))"
