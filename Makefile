.PHONY: wavie cli-local vet test dist clean

WAVIE_BIN := wavie
WAVIE_SRC := ./cmd/wavie
VERSION ?= 0.1.0-dev
MODULE  := github.com/bitwave-io/bitwave-cli
LDFLAGS := -s -w \
	-X $(MODULE)/internal/wavie/cmd.Version=$(VERSION)

# cli-local pins backend service URLs to localhost so commands like
# `wavie wallets sync` and `wavie share` work against a locally-running stack
# without --base-url or env vars. Override the individual URLs to point
# elsewhere (e.g. LOCAL_GL_URL=http://10.0.0.5:4073 make cli-local).
LOCAL_BQ_URL   ?= http://localhost:8080
LOCAL_GL_URL   ?= http://localhost:4073
LOCAL_CORE_URL ?= http://localhost:4073
LOCAL_LDFLAGS := $(LDFLAGS) \
	-X $(MODULE)/internal/wavie/cmd.defaultBlockchainQueryBaseURL=$(LOCAL_BQ_URL) \
	-X $(MODULE)/internal/wavie/cmd.defaultGLBaseURL=$(LOCAL_GL_URL) \
	-X $(MODULE)/internal/wavie/cmd.defaultCoreBaseURL=$(LOCAL_CORE_URL)

DIST := dist

# Platforms to cross-compile for distribution.
PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64

# wavie: build the wavie binary (workspace-first plain-text accounting).
wavie:
	go build -ldflags "$(LDFLAGS)" -o $(WAVIE_BIN) $(WAVIE_SRC)

# cli-local: build wavie with localhost defaults for backend services.
# Useful while running gl-svc / blockchain-query-svc next to it.
cli-local:
	go build -ldflags "$(LOCAL_LDFLAGS)" -o $(WAVIE_BIN) $(WAVIE_SRC)
	@echo "Built $(WAVIE_BIN) with local defaults:"
	@echo "  blockchain-query-svc -> $(LOCAL_BQ_URL)"
	@echo "  gl-svc               -> $(LOCAL_GL_URL)"
	@echo "  core-svc             -> $(LOCAL_CORE_URL)"

vet:
	go vet ./...

test:
	go test ./...

# Cross-compile for all platforms. Produces dist/wavie-<os>-<arch>[.exe].
dist: clean
	@mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		out="$(DIST)/$(WAVIE_BIN)-$${os}-$${arch}$${ext}"; \
		echo "Building $$out ..."; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -ldflags "$(LDFLAGS)" -o "$$out" $(WAVIE_SRC) || exit 1; \
	done
	@echo "All binaries written to $(DIST)/"

clean:
	rm -f $(WAVIE_BIN)
	rm -rf $(DIST)
