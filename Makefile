# All development tools are declared as `tool` directives in go.mod and invoked
# through `go tool`, so a fresh checkout needs nothing installed but Go itself.

GO ?= go
BIN := bin/travelmap

.PHONY: build test lint fmt vulncheck run migrate clean

build:
	$(GO) build -o $(BIN) ./cmd/travelmap

test:
	$(GO) test ./... -race -cover -shuffle=on

lint:
	$(GO) tool golangci-lint run
	@$(GO) mod tidy -diff || { echo "go.mod / go.sum: not tidy, run 'go mod tidy'"; exit 1; }
	@set -e; unformatted="$$($(GO) tool gofumpt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofumpt: not formatted, run 'make fmt':"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

fmt:
	$(GO) tool gofumpt -w .

# CI only: the development container's egress proxy blocks vuln.go.dev.
vulncheck:
	$(GO) tool govulncheck ./...

run:
	$(GO) run ./cmd/travelmap serve

migrate:
	$(GO) run ./cmd/travelmap migrate

clean:
	rm -rf bin
