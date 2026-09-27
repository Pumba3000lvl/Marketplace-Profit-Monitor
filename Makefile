.PHONY: build frontend backend test clean

GO_DOCKER = docker run --rm -v "$(CURDIR):/workspace" -v marketplace-go-mod-cache:/go/pkg/mod -v marketplace-go-build-cache:/root/.cache/go-build -w /workspace golang:1.23

build: frontend backend

frontend:
	npm run build

backend:
	mkdir -p dist
	if command -v go >/dev/null 2>&1; then \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$(go env GOARCH) go build -trimpath -o dist/gpx_marketplace_profit_linux_$$(go env GOARCH) ./pkg; \
	else \
		$(GO_DOCKER) sh -c 'CGO_ENABLED=0 GOOS=linux GOARCH=$$(go env GOARCH) go build -trimpath -o dist/gpx_marketplace_profit_linux_$$(go env GOARCH) ./pkg'; \
	fi

test:
	if command -v go >/dev/null 2>&1; then \
		go test ./pkg/...; \
	else \
		$(GO_DOCKER) go test ./pkg/...; \
	fi

clean:
	rm -rf dist
