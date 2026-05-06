# Makefile — build the wllr binary and WASM extensions.
#
# make             — build the wllr binary (requires extensions to be built first)
# make extensions  — build WASM extensions → cmd/builtins/*.wasm + install optional extensions
# make all         — build extensions then the binary
# make clean       — remove dist/ and cmd/builtins/*.wasm
#
# Built-in extensions (embedded in the binary):
#   readfile, writefile, exec, env, agents
#
# Installed extensions (loaded from ~/.wllr/extensions/ at runtime):
#   context, skills

DIST_DIR    := dist
BINARY      := $(DIST_DIR)/wllr
BUILTINS    := cmd/builtins
EXT_DIR     := $(HOME)/.wllr/extensions

.PHONY: all build extensions clean

all: extensions build

# build depends on extensions so the embedded WASM files are present.
build: extensions $(DIST_DIR)
	go build -o $(BINARY) ./cmd/
	@echo "Built $(BINARY)"

# extensions builds all WASM extensions.
# Built-ins go to cmd/builtins/ (embedded in the binary).
# Optional extensions are installed to ~/.wllr/extensions/<name>/.
extensions: $(DIST_DIR) $(BUILTINS)
	cd extensions/readfile  && GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(CURDIR)/$(BUILTINS)/readfile.wasm .
	cd extensions/writefile && GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(CURDIR)/$(BUILTINS)/writefile.wasm .
	cd extensions/exec      && GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(CURDIR)/$(BUILTINS)/exec.wasm .
	cd extensions/env       && GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(CURDIR)/$(BUILTINS)/env.wasm .
	cd extensions/agents    && GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(CURDIR)/$(BUILTINS)/agents.wasm .
	mkdir -p $(EXT_DIR)/context $(EXT_DIR)/skills
	cd extensions/context   && GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(EXT_DIR)/context/context.wasm .
	cp extensions/context/context.json $(EXT_DIR)/context/
	cd extensions/skills    && GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(EXT_DIR)/skills/skills.wasm .
	cp extensions/skills/skills.json $(EXT_DIR)/skills/
	@echo "Built all extensions"

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

$(BUILTINS):
	mkdir -p $(BUILTINS)

clean:
	rm -rf $(DIST_DIR)
	rm -f $(BUILTINS)/readfile.wasm $(BUILTINS)/writefile.wasm $(BUILTINS)/exec.wasm $(BUILTINS)/env.wasm $(BUILTINS)/agents.wasm
