//go:build !wasip1

package main

// This package targets wasip1; main.go (which defines func main) is
// wasip1-only. The untagged sessionio.go/data files are compiled on the host
// for unit tests, which makes `go build ./...` attempt a host build of package
// main — so provide a no-op main for the host so that build succeeds. The real
// extension entrypoint is the wasip1 main in main.go.
func main() {}
