.PHONY: bitwave cli-local vet test dist clean

BITWAVE_BIN := bitwave
BITWAVE_SRC := ./cmd/bitwave
VERSION ?= 0.1.0-dev
MODULE  := github.com/bitwave-io/bitwave-cli
LDFLAGS := -s -w \
	-X $(MODULE)/internal/bitwave/cmd.Version=$(VERSION)

# cli-local pins backend service URLs to localhost so commands like
# `bitwave wallets sync` and `bitwave share` work against a locally-running stack
# without --base-url or env vars. Override the individual URLs to point
# elsewhere (e.g. LOCAL_GL_URL=http://10.0.0.5:4073 make cli-local).
LOCAL_BQ_URL   ?= http://localhost:8080
LOCAL_GL_URL   ?= http://localhost:4073
LOCAL_CORE_URL ?= http://localhost:4073
LOCAL_LDFLAGS := $(LDFLAGS) \
	-X $(MODULE)/internal/bitwave/cmd.defaultBlockchainQueryBaseURL=$(LOCAL_BQ_URL) \
	-X $(MODULE)/internal/bitwave/cmd.defaultGLBaseURL=$(LOCAL_GL_URL) \
	-X $(MODULE)/internal/bitwave/cmd.defaultCoreBaseURL=$(LOCAL_CORE_URL)

DIST := dist

# Platforms to cross-compile for distribution.
PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64

# bitwave: build the bitwave binary (workspace-first plain-text accounting).
bitwave:
	go build -ldflags "$(LDFLAGS)" -o $(BITWAVE_BIN) $(BITWAVE_SRC)

# cli-local: build bitwave with localhost defaults for backend services.
# Useful while running the cloud ledger / the blockchain query API next to it.
cli-local:
	go build -ldflags "$(LOCAL_LDFLAGS)" -o $(BITWAVE_BIN) $(BITWAVE_SRC)
	@echo "Built $(BITWAVE_BIN) with local defaults:"
	@echo "  the blockchain query API -> $(LOCAL_BQ_URL)"
	@echo "  the cloud ledger               -> $(LOCAL_GL_URL)"
	@echo "  the Bitwave core API             -> $(LOCAL_CORE_URL)"

vet:
	go vet ./...

test:
	go test ./...

# Cross-compile for all platforms. Produces dist/bitwave-<os>-<arch>[.exe].
dist: clean
	@mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		out="$(DIST)/$(BITWAVE_BIN)-$${os}-$${arch}$${ext}"; \
		echo "Building $$out ..."; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -ldflags "$(LDFLAGS)" -o "$$out" $(BITWAVE_SRC) || exit 1; \
	done
	@echo "All binaries written to $(DIST)/"

clean:
	rm -f $(BITWAVE_BIN)
	rm -rf $(DIST)
