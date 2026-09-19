GO ?= go
EXECUTABLE := gitea-mcp
VERSION ?= $(shell git describe --tags --always | sed 's/-/+/' | sed 's/^v//')
LDFLAGS := -X "main.Version=$(VERSION)"

GOLANGCI_LINT_PACKAGE ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 # renovate: datasource=go
GOVULNCHECK_PACKAGE ?= golang.org/x/vuln/cmd/govulncheck@v1.8.0 # renovate: datasource=go

GOTEST_FLAGS ?= -race -timeout 20m

.PHONY: help
help: ## print this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: install
install: build ## install the application
	@echo "Installing $(EXECUTABLE)..."
	@mkdir -p $(GOPATH)/bin
	@cp $(EXECUTABLE) $(GOPATH)/bin/$(EXECUTABLE)
	@echo "Installed $(EXECUTABLE) to $(GOPATH)/bin/$(EXECUTABLE)"
	@echo "Please add $(GOPATH)/bin to your PATH if it is not already there."

.PHONY: uninstall
uninstall: ## uninstall the application
	@echo "Uninstalling $(EXECUTABLE)..."
	@rm -f $(GOPATH)/bin/$(EXECUTABLE)
	@echo "Uninstalled $(EXECUTABLE) from $(GOPATH)/bin/$(EXECUTABLE)"

.PHONY: clean
clean: ## delete build artifacts
	@echo "Cleaning up build artifacts..."
	@rm -f $(EXECUTABLE)
	@echo "Cleaned up $(EXECUTABLE)"

.PHONY: build
build: ## build the application
	$(GO) build -v -ldflags '-s -w $(LDFLAGS)' -o $(EXECUTABLE)

.PHONY: test
test: ## run Go tests
	$(GO) test $(GOTEST_FLAGS) ./...

.PHONY: air
air: ## install air for hot reload
	@hash air > /dev/null 2>&1; if [ $$? -ne 0 ]; then \
		$(GO) install github.com/air-verse/air@latest; \
	fi

.PHONY: dev
dev: air ## run the application with hot reload
	air --build.cmd "make build" --build.bin ./gitea-mcp

.PHONY: fmt
fmt: ## format the Go code
	$(GO) run $(GOLANGCI_LINT_PACKAGE) fmt

.PHONY: fmt-check
fmt-check: fmt ## check that Go code is formatted
	@diff=$$(git diff --color=always); \
	if [ -n "$$diff" ]; then \
		echo "Please run 'make fmt' and commit the result:"; \
		printf "%s" "$${diff}"; \
		exit 1; \
	fi

.PHONY: lint
lint: lint-go ## lint everything

.PHONY: lint-fix
lint-fix: lint-go-fix ## lint everything and fix issues

.PHONY: lint-go
lint-go: ## lint go files
	$(GO) run $(GOLANGCI_LINT_PACKAGE) run

.PHONY: lint-go-fix
lint-go-fix: ## lint go files and fix issues
	$(GO) run $(GOLANGCI_LINT_PACKAGE) run --fix

.PHONY: security-check
security-check: ## run security check
	$(GO) run $(GOVULNCHECK_PACKAGE) -show color ./... || true

.PHONY: tidy
tidy: ## run go mod tidy
	$(eval MIN_GO_VERSION := $(shell grep -Eo '^go\s+[0-9]+\.[0-9.]+' go.mod | cut -d' ' -f2))
	$(eval GO_TOOLCHAIN := $(shell grep -Eo '^toolchain\s+go[0-9.]+' go.mod | cut -d' ' -f2))
	$(GO) mod tidy -compat=$(MIN_GO_VERSION)
	@# workaround https://github.com/golang/go/issues/75331: restore toolchain if tidy dropped it
	@if [ -n "$(GO_TOOLCHAIN)" ] && ! grep -qE '^toolchain\s' go.mod; then \
		$(GO) mod edit -toolchain=$(GO_TOOLCHAIN); \
	fi

.PHONY: vendor
vendor: tidy ## tidy and verify module dependencies
	$(GO) mod verify
