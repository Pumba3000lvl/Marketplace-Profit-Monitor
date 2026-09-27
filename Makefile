.PHONY: build frontend backend backend-native backend-all test clean

GO_DOCKER = docker run --rm -v "$(CURDIR):/workspace" -v marketplace-go-mod-cache:/go/pkg/mod -v marketplace-go-build-cache:/root/.cache/go-build -w /workspace golang:1.23.5
TARGET_OS ?= linux
TARGET_ARCH ?= $(shell go env GOARCH 2>/dev/null || uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TARGET_EXT = $(if $(filter windows,$(TARGET_OS)),.exe,)
BACKEND_OUTPUT = dist/gpx_marketplace_profit_$(TARGET_OS)_$(TARGET_ARCH)$(TARGET_EXT)

build: frontend backend

frontend:
	npm run build

backend:
	mkdir -p dist
	if command -v go >/dev/null 2>&1; then \
		CGO_ENABLED=0 GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) go build -trimpath -o $(BACKEND_OUTPUT) ./pkg; \
	else \
		$(GO_DOCKER) sh -c 'CGO_ENABLED=0 GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) go build -trimpath -o $(BACKEND_OUTPUT) ./pkg'; \
	fi
	if [ "$(TARGET_OS)" != "windows" ]; then chmod 755 $(BACKEND_OUTPUT); fi

backend-native:
	$(MAKE) backend TARGET_OS=$$(go env GOOS) TARGET_ARCH=$$(go env GOARCH)

backend-all:
	for target_os in linux darwin windows; do \
		for target_arch in amd64 arm64; do \
			$(MAKE) --no-print-directory backend TARGET_OS=$$target_os TARGET_ARCH=$$target_arch || exit $$?; \
		done; \
	done

test:
	if command -v go >/dev/null 2>&1; then \
		go test ./pkg/...; \
	else \
		$(GO_DOCKER) go test ./pkg/...; \
	fi

clean:
	rm -rf dist
