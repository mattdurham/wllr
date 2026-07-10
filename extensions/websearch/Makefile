# Makefile for building the websearch extension

WASM_OUTPUT = websearch.wasm

# Build using TinyGo via Docker (requires Docker)
build:
	docker run --rm -v "${PWD}":/workspace -w /workspace tinygo/tinygo:latest \
		tinygo build -o $(WASM_OUTPUT) -target wasip1 ./...

# Clean build artifacts
clean:
	rm -f $(WASM_OUTPUT)

.PHONY: build clean
