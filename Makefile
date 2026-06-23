.PHONY: bwx cli-local vet test dist clean

BWX_BIN := bwx
BWX_SRC := ./cmd/bwx
VERSION ?= 0.1.0-dev
MODULE  := github.com/bitwave-io/bitwave-cli
LDFLAGS := -s -w \
	-X $(MODULE)/internal/bwx/cmd.Version=$(VERSION)

# cli-local pins backend service URLs to localhost so commands like
# `bwx wallets sync` and `bwx share` work against a locally-running stack
# without --base-url or env vars. Override the individual URLs to point
# elsewhere (e.g. LOCAL_GL_URL=http://10.0.0.5:4073 make cli-local).
LOCAL_BQ_URL   ?= http://localhost:8080
LOCAL_GL_URL   ?= http://localhost:4073
LOCAL_CORE_URL ?= http://localhost:4073
LOCAL_LDFLAGS := $(LDFLAGS) \
	-X $(MODULE)/internal/bwx/cmd.defaultBlockchainQueryBaseURL=$(LOCAL_BQ_URL) \
	-X $(MODULE)/internal/bwx/cmd.defaultGLBaseURL=$(LOCAL_GL_URL) \
	-X $(MODULE)/internal/bwx/cmd.defaultCoreBaseURL=$(LOCAL_CORE_URL)

DIST := dist

# Platforms to cross-compile for distribution.
PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64

# bwx: build the bwx binary (workspace-first plain-text accounting).
bwx:
	go build -ldflags "$(LDFLAGS)" -o $(BWX_BIN) $(BWX_SRC)

# cli-local: build bwx with localhost defaults for backend services.
# Useful while running gl-svc / blockchain-query-svc next to it.
cli-local:
	go build -ldflags "$(LOCAL_LDFLAGS)" -o $(BWX_BIN) $(BWX_SRC)
	@echo "Built $(BWX_BIN) with local defaults:"
	@echo "  blockchain-query-svc -> $(LOCAL_BQ_URL)"
	@echo "  gl-svc               -> $(LOCAL_GL_URL)"
	@echo "  core-svc             -> $(LOCAL_CORE_URL)"

vet:
	go vet ./...

test:
	go test ./...

# Cross-compile for all platforms. Produces dist/bwx-<os>-<arch>[.exe].
dist: clean
	@mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		out="$(DIST)/$(BWX_BIN)-$${os}-$${arch}$${ext}"; \
		echo "Building $$out ..."; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -ldflags "$(LDFLAGS)" -o "$$out" $(BWX_SRC) || exit 1; \
	done
	@echo "All binaries written to $(DIST)/"

clean:
	rm -f $(BWX_BIN)
	rm -rf $(DIST)
