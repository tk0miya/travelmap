# All Go development tools are declared as `tool` directives in go.mod and
# invoked through `go tool`, so a fresh checkout needs nothing installed but
# Go itself and Node — see docs/toolchain.md's "Frontend toolchain" for why
# the frontend build needs the latter.

GO ?= go
BIN := bin/travelmap
FRONTEND_DIR := frontend
FRONTEND_DIST := internal/httpapi/frontend/dist
FRONTEND_NODE_MODULES := $(FRONTEND_DIR)/node_modules/.package-lock.json

.PHONY: build test lint fmt check vulncheck run migrate clean \
	frontend frontend-lint frontend-test

build: frontend
	$(GO) build -o $(BIN) ./cmd/travelmap

# go:embed (internal/httpapi/frontend.go) requires $(FRONTEND_DIST) to exist,
# so every target that builds or tests Go code depends on this first.
test: frontend
	$(GO) test ./... -race -cover -shuffle=on

lint: frontend
	$(GO) tool golangci-lint run
	@$(GO) mod tidy -diff || { echo "go.mod / go.sum: not tidy, run 'go mod tidy'"; exit 1; }
	@set -e; unformatted="$$($(GO) tool gofumpt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofumpt: not formatted, run 'make fmt':"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

fmt: $(FRONTEND_NODE_MODULES)
	$(GO) tool gofumpt -w .
	cd $(FRONTEND_DIR) && npm run format

# lint + test, plus the frontend's own lint and test: what the pre-commit
# hook runs. Deliberately not the CI set, which also runs `vulncheck`.
check: lint test frontend-lint frontend-test

# CI only: the development container's egress proxy blocks vuln.go.dev.
vulncheck: frontend
	$(GO) tool govulncheck ./...

run: frontend
	$(GO) run ./cmd/travelmap serve

migrate:
	$(GO) run ./cmd/travelmap migrate

clean:
	rm -rf bin $(FRONTEND_DIST)

# $(FRONTEND_NODE_MODULES) is a file npm itself writes on install, used here
# as a cheap "already installed" marker so repeated targets in one `make
# check` do not each pay for their own `npm ci`.
$(FRONTEND_NODE_MODULES): $(FRONTEND_DIR)/package-lock.json
	cd $(FRONTEND_DIR) && npm ci

frontend: $(FRONTEND_NODE_MODULES)
	cd $(FRONTEND_DIR) && npm run build

frontend-lint: $(FRONTEND_NODE_MODULES)
	cd $(FRONTEND_DIR) && npm run lint

frontend-test: $(FRONTEND_NODE_MODULES)
	cd $(FRONTEND_DIR) && npm test
