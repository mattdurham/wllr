# Makefile — build the wllr binary and WASM extensions.
#
# make             — build the wllr binary (requires extensions to be built first)
# make extensions  — build WASM extensions → cmd/builtins/*.wasm
# make all         — build extensions then the binary
# make clean       — remove dist/ and cmd/builtins/*.wasm

DIST_DIR  := dist
BINARY    := $(DIST_DIR)/wllr
BUILTINS  := cmd/builtins

.PHONY: all build extensions clean

all: extensions build

# build depends on extensions so the embedded WASM files are present.
build: extensions $(DIST_DIR)
	go build -o $(BINARY) ./cmd/
	@echo "Built $(BINARY)"

# extensions builds all WASM extensions embedded in the binary.
extensions: $(DIST_DIR) $(BUILTINS)
	cd extensions/readfile  && GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(CURDIR)/$(BUILTINS)/readfile.wasm .
	cd extensions/writefile && GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(CURDIR)/$(BUILTINS)/writefile.wasm .
	cd extensions/exec      && GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(CURDIR)/$(BUILTINS)/exec.wasm .
	cd extensions/env       && GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(CURDIR)/$(BUILTINS)/env.wasm .
	cd extensions/agents    && GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $(CURDIR)/$(BUILTINS)/agents.wasm .
	@echo "Built all extensions"

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

$(BUILTINS):
	mkdir -p $(BUILTINS)

clean:
	rm -rf $(DIST_DIR)
	rm -f $(BUILTINS)/readfile.wasm $(BUILTINS)/writefile.wasm $(BUILTINS)/exec.wasm $(BUILTINS)/env.wasm $(BUILTINS)/agents.wasm
