# Makefile — build the wllr binary and WASM extensions.
#
# Build targets:
#   make             — build built-in WASM extensions, then the wllr binary
#   make builtins    — build embedded WASM extensions → cmd/builtins/*.wasm
#   make extensions  — build built-ins + install optional extensions
#   make install     — build and install wllr plus extensions
#   make all         — build extensions then the binary
#   WASM_COMPILER=go make build      — force standard Go WASM builds
#   WASM_COMPILER=tinygo make build  — require TinyGo WASM builds
#   make clean       — remove dist/ and cmd/builtins/*.wasm
#
# Development targets:
#   make test             — run all unit tests
#   make format           — format all Go code with gofumpt
#   make format-all       — auto-fix all formatting issues (gofumpt, golines, betteralign)
#   make lint             — run golangci-lint (40+ linters)
#   make nilaway          — run nil safety checks
#   make betteralign      — check struct alignment
#   make betteralign-fix  — auto-fix struct alignment
#   make gofumpt-check    — check formatting (CI)
#   make gofumpt          — auto-fix formatting
#   make golines-check    — check line length (CI)
#   make golines          — auto-fix line length
#   make deadcode         — check for unreachable code
#   make staticcheck      — run staticcheck (additional static analysis)
#   make install-tools    — install all code quality tools
#   make ci               — run full CI pipeline locally
#   make precommit        — run build and all quality checks (REQUIRED before commit)
#
# Built-in extensions (embedded in the binary):
#   agents, history, logging, plan, queue, sigil, statusline
#   (read_file, write_file, exec, get_env are native Go — no WASM build needed)
#
# Installed extensions (loaded from ~/.wllr/extensions/ at runtime):
#   context, skills, tasks, lsp, memory, permissions, mcp-bridge, otel-traces

DIST_DIR    := dist
BINARY      := $(DIST_DIR)/wllr
BUILTINS    := cmd/builtins
INSTALL_BIN ?= $(HOME)/.local/bin
EXT_DIR     := $(HOME)/.wllr/extensions
GOCACHE     ?= $(CURDIR)/.cache/go-build
GOLANGCI_LINT_CACHE ?= $(CURDIR)/.cache/golangci-lint
WASM_COMPILER ?= auto
TINYGO ?= tinygo
TINYGO_MODE ?= docker
TINYGO_IMAGE ?= tinygo/tinygo:latest
TINYGO_FLAGS ?= -buildmode=c-shared -target=wasi -opt=z
export GOCACHE
export GOLANGCI_LINT_CACHE

WASM_BUILD = WASM_COMPILER=$(WASM_COMPILER) TINYGO_MODE=$(TINYGO_MODE) TINYGO=$(TINYGO) TINYGO_IMAGE=$(TINYGO_IMAGE) TINYGO_FLAGS="$(TINYGO_FLAGS)" scripts/build-wasm-extension.sh

# Package list - lazy evaluation
PACKAGES = $(shell go list -e -f '{{if .GoFiles}}{{.ImportPath}}{{end}}' ./...)

.DEFAULT_GOAL := build

.PHONY: all build builtins extensions optional-extensions install clean clean-extensions lint test format precommit ci install-tools nilaway betteralign betteralign-fix gofumpt-check gofumpt golines-check golines format-all deadcode staticcheck docs-check generate-models

all: extensions build

# build depends on builtins so the embedded WASM files are present.
build: builtins $(DIST_DIR)
	go build -o $(BINARY) ./cmd/
	@echo "Built $(BINARY)"

install: extensions build
	mkdir -p "$(INSTALL_BIN)"
	install -m 755 $(BINARY) "$(INSTALL_BIN)/wllr"
	@echo "Installed wllr to $(INSTALL_BIN)/wllr"

# builtins builds embedded WASM extensions.
builtins: $(DIST_DIR) $(BUILTINS)
	$(WASM_BUILD) $(BUILTINS)/agents.wasm extensions/agents
	$(WASM_BUILD) $(BUILTINS)/history.wasm extensions/history
	$(WASM_BUILD) $(BUILTINS)/logging.wasm extensions/logging
	$(WASM_BUILD) $(BUILTINS)/plan.wasm extensions/plan
	$(WASM_BUILD) $(BUILTINS)/queue.wasm extensions/queue
	$(WASM_BUILD) $(BUILTINS)/sigil.wasm extensions/sigil
	$(WASM_BUILD) $(DIST_DIR)/statusline.wasm extensions/statusline
	cp $(DIST_DIR)/statusline.wasm $(BUILTINS)/statusline.wasm
	@echo "Built built-in extensions"
# Optional extensions are installed to ~/.wllr/extensions/<name>/.
extensions: builtins optional-extensions
	@echo "Built all extensions"

optional-extensions:
	mkdir -p $(EXT_DIR)/websearch
	mkdir -p $(EXT_DIR)/context $(EXT_DIR)/skills $(EXT_DIR)/tasks $(EXT_DIR)/lsp $(EXT_DIR)/memory $(EXT_DIR)/permissions $(EXT_DIR)/mcp-bridge $(EXT_DIR)/otel-traces $(EXT_DIR)/websearch
	$(WASM_BUILD) $(EXT_DIR)/context/context.wasm extensions/context
	cp extensions/context/context.json $(EXT_DIR)/context/
	$(WASM_BUILD) $(EXT_DIR)/skills/skills.wasm extensions/skills
	cp extensions/skills/skills.json $(EXT_DIR)/skills/
	$(WASM_BUILD) $(EXT_DIR)/tasks/tasks.wasm extensions/tasks
	cp extensions/tasks/tasks.json $(EXT_DIR)/tasks/
	$(WASM_BUILD) $(EXT_DIR)/lsp/lsp.wasm extensions/lsp
	cp extensions/lsp/extension.yaml $(EXT_DIR)/lsp/
	$(WASM_BUILD) $(EXT_DIR)/memory/memory.wasm extensions/memory
	cp extensions/memory/extension.yaml $(EXT_DIR)/memory/
	$(WASM_BUILD) $(EXT_DIR)/permissions/permissions.wasm extensions/permissions
	$(WASM_BUILD) $(EXT_DIR)/mcp-bridge/mcp-bridge.wasm extensions/mcp-bridge
	cp extensions/permissions/extension.yaml $(EXT_DIR)/permissions/
	cp extensions/mcp-bridge/mcp-bridge.json $(EXT_DIR)/mcp-bridge/
	$(WASM_BUILD) $(EXT_DIR)/otel-traces/otel-traces.wasm extensions/otel-traces
	cp extensions/otel-traces/extension.yaml $(EXT_DIR)/otel-traces/
	cp extensions/otel-traces/otel-traces.json $(EXT_DIR)/otel-traces/
	$(WASM_BUILD) $(EXT_DIR)/websearch/websearch.wasm extensions/websearch
	cp extensions/websearch/websearch.json $(EXT_DIR)/websearch/
	@echo "Installed optional extensions to $(EXT_DIR)"

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

$(BUILTINS):
	mkdir -p $(BUILTINS)

# Install all code quality tools
install-tools:
	@echo "==> Installing code quality tools..."
	@go install go.uber.org/nilaway/cmd/nilaway@latest
	@go install github.com/dkorunic/betteralign/cmd/betteralign@latest
	@go install mvdan.cc/gofumpt@latest
	@go install github.com/segmentio/golines@latest
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@go install golang.org/x/tools/cmd/deadcode@latest
	@go install honnef.co/go/tools/cmd/staticcheck@latest
	@echo "==> All tools installed!"

# Nil safety checks
nilaway:
	@echo "==> Running nil safety checks (nilaway)..."
	@nilaway -include-pkgs "github.com/modernice/wllr/..." ./...
	@echo "==> Nil safety checks passed!"

# Struct alignment checks
betteralign:
	@echo "==> Checking struct alignment..."
	@betteralign ./...
	@echo "==> Struct alignment check passed!"

# Auto-fix struct alignment
betteralign-fix:
	@echo "==> Auto-fixing struct alignment..."
	@betteralign -apply ./... || true
	@echo "==> Struct alignment fixed!"

# Run deadcode check
deadcode:
	@echo "==> Running deadcode analysis..."
	@echo "    Building main to anchor public APIs..."
	@go build -o /dev/null ./cmd/
	@echo "    Running deadcode tool..."
	@# Filter excludes internal/memory (compiled as WASM extension) and testutil (test-only helpers).
	@# Both are unreachable from the main binary by design.
	@output=$$(deadcode -test ./... | grep -v -e 'internal/memory' -e 'testutil/'); \
	if [ -n "$$output" ]; then \
		echo "❌ Deadcode found:"; \
		echo "$$output"; \
		exit 1; \
	fi
	@echo "==> Deadcode check passed!"

# Run staticcheck
staticcheck:
	@echo "==> Running staticcheck..."
	@staticcheck -f stylish ./...
	@echo "==> Staticcheck passed!"

# Check that statically registered native and bundled extension tools have
# discoverable input/output contract docs.
docs-check:
	@echo "==> Checking tool contract docs..."
	@go run scripts/check-tool-contracts.go
	@echo "==> Tool contract docs complete!"

# Check formatting with gofumpt (CI mode)
gofumpt-check:
	@echo "==> Checking formatting (gofumpt)..."
	@output=$$(gofumpt -l . 2>&1); \
	if [ -n "$$output" ]; then \
		echo "❌ Code is not formatted. Run 'make gofumpt' to fix:"; \
		echo "$$output"; \
		exit 1; \
	fi
	@echo "==> Formatting check passed!"

# Auto-fix formatting with gofumpt
gofumpt:
	@echo "==> Formatting Go code (gofumpt)..."
	@gofumpt -w .
	@echo "==> Code formatted!"

# Check line length (CI mode)
golines-check:
	@echo "==> Checking line length (120 chars)..."
	@output=$$(golines --dry-run --max-len=120 --base-formatter=gofumpt . 2>&1 | grep -v "^$$"); \
	if [ -n "$$output" ]; then \
		echo "❌ Line length violations found. Run 'make golines' to fix:"; \
		echo "$$output"; \
		exit 1; \
	fi
	@echo "==> Line length check passed!"

# Auto-fix line length
golines:
	@echo "==> Fixing line length violations..."
	@golines -w --max-len=120 --base-formatter=gofumpt .
	@echo "==> Line length fixed!"

# Auto-fix all formatting issues
format-all: gofumpt golines betteralign-fix
	@echo "==> All auto-fixable issues resolved!"

# Format all Go code
format:
	@echo "==> Formatting Go code (gofumpt)..."
	@gofumpt -w .
	@echo "==> Code formatted!"

# Run linters
lint:
	@echo "==> Running golangci-lint (40+ linters)..."
	@golangci-lint run
	@echo "==> Linting complete!"

# Run tests
test:
	@echo "==> Running unit tests..."
	@go test -v -race -timeout=10m $(PACKAGES)
	@echo "==> All tests passed!"

# Run integration tests (requires ANTHROPIC_API_KEY)
test-integration:
	@echo "==> Running integration tests (requires ANTHROPIC_API_KEY)..."
	@go test -tags integration -v -timeout=120s ./test/integration/...
	@echo "==> Integration tests passed!"

# Run full CI pipeline locally
ci:
	@echo "🔄 Running CI pipeline locally..."
	@echo ""
	@PASS=0; FAIL=0; \
	echo "── [1/7] Format check (gofumpt)"; \
	if $(MAKE) gofumpt-check > /tmp/wllr-ci-gofumpt.log 2>&1; then \
		echo "   ✅ PASS"; PASS=$$((PASS + 1)); \
	else \
		echo "   ❌ FAIL"; tail -20 /tmp/wllr-ci-gofumpt.log | sed 's/^/   /'; FAIL=$$((FAIL + 1)); \
	fi; \
	echo "── [2/7] Line length check (golines)"; \
	if $(MAKE) golines-check > /tmp/wllr-ci-golines.log 2>&1; then \
		echo "   ✅ PASS"; PASS=$$((PASS + 1)); \
	else \
		echo "   ❌ FAIL"; tail -20 /tmp/wllr-ci-golines.log | sed 's/^/   /'; FAIL=$$((FAIL + 1)); \
	fi; \
	echo "── [3/7] Lint (golangci-lint - 40+ linters)"; \
	if $(MAKE) lint > /tmp/wllr-ci-lint.log 2>&1; then \
		echo "   ✅ PASS"; PASS=$$((PASS + 1)); \
	else \
		echo "   ❌ FAIL"; tail -30 /tmp/wllr-ci-lint.log | sed 's/^/   /'; FAIL=$$((FAIL + 1)); \
	fi; \
	echo "── [4/7] Struct alignment (betteralign)"; \
	if $(MAKE) betteralign > /tmp/wllr-ci-betteralign.log 2>&1; then \
		echo "   ✅ PASS"; PASS=$$((PASS + 1)); \
	else \
		echo "   ❌ FAIL"; tail -20 /tmp/wllr-ci-betteralign.log | sed 's/^/   /'; FAIL=$$((FAIL + 1)); \
	fi; \
	echo "── [5/7] Tests"; \
	if $(MAKE) test > /tmp/wllr-ci.log 2>&1; then \
		echo "   ✅ PASS"; PASS=$$((PASS + 1)); \
	else \
		echo "   ❌ FAIL"; tail -30 /tmp/wllr-ci.log | sed 's/^/   /'; FAIL=$$((FAIL + 1)); \
	fi; \
	echo "── [6/7] Deadcode check"; \
	if $(MAKE) deadcode > /tmp/wllr-ci-deadcode.log 2>&1; then \
		echo "   ✅ PASS"; PASS=$$((PASS + 1)); \
	else \
		echo "   ❌ FAIL"; tail -20 /tmp/wllr-ci-deadcode.log | sed 's/^/   /'; FAIL=$$((FAIL + 1)); \
	fi; \
	echo "── [7/7] Staticcheck"; \
	if $(MAKE) staticcheck > /tmp/wllr-ci-staticcheck.log 2>&1; then \
		echo "   ✅ PASS"; PASS=$$((PASS + 1)); \
	else \
		echo "   ❌ FAIL"; tail -20 /tmp/wllr-ci-staticcheck.log | sed 's/^/   /'; FAIL=$$((FAIL + 1)); \
	fi; \
	rm -f /tmp/wllr-ci*.log; \
	echo ""; \
	echo "── Summary: $$PASS passed, $$FAIL failed"; \
	if [ "$$FAIL" -gt 0 ]; then \
		echo "❌ CI pipeline FAILED"; exit 1; \
	else \
		echo "✅ CI pipeline PASSED"; \
	fi

# Run all pre-commit quality checks
precommit:
	@echo "==> Running pre-commit quality checks..."
	@echo ""
	@echo "[1/10] Auto-formatting code (gofumpt)..."
	@$(MAKE) gofumpt
	@echo "✅ gofumpt: formatted"
	@echo ""
	@echo "[2/10] Auto-fixing line length (golines)..."
	@$(MAKE) golines
	@echo "✅ golines: fixed"
	@echo ""
	@echo "[3/10] Running golangci-lint (40+ linters)..."
	@$(MAKE) lint
	@echo "✅ golangci-lint: clean"
	@echo ""
	@echo "[4/10] Checking struct alignment (betteralign)..."
	@$(MAKE) betteralign
	@echo "✅ betteralign: clean"
	@echo ""
	@echo "[5/10] Running nil safety checks (nilaway)..."
	@$(MAKE) nilaway
	@echo "✅ nilaway: clean"
	@echo ""
	@echo "[6/10] Building project..."
	@$(MAKE) build
	@echo "✅ build: successful"
	@echo ""
	@echo "[7/10] Running tests..."
	@$(MAKE) test
	@echo "✅ tests: all passing"
	@echo ""
	@echo "[8/10] Running tool contract docs check..."
	@$(MAKE) docs-check
	@echo "✅ docs-check: tool contracts complete"
	@echo ""
	@echo "[9/10] Running deadcode check..."
	@$(MAKE) deadcode
	@echo "✅ deadcode: no unreachable code"
	@echo ""
	@echo "[10/10] Running staticcheck..."
	@$(MAKE) staticcheck
	@echo "✅ staticcheck: clean"
	@echo ""
	@echo "✅ All pre-commit checks passed!"
	@echo "✅ Ready to commit"

# Generate agent/models.generated.go from Anthropic API
generate-models:
	@echo "==> Generating model context window table..."
	@ANTHROPIC_API_KEY=$${ANTHROPIC_API_KEY} go run scripts/generate-models.go
	@echo "==> Done — commit agent/models.generated.go"

clean:
	rm -rf $(DIST_DIR)
	rm -f $(BUILTINS)/*.wasm

clean-extensions:
	rm -rf $(EXT_DIR)/memory $(EXT_DIR)/permissions $(EXT_DIR)/mcp-bridge $(EXT_DIR)/otel-traces
