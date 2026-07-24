package main

import "github.com/jpaddison3/dharma/internal/cli"

// version is stamped at build time via -ldflags="-X main.version=...";
// see scripts/lib.sh's build_dharma.
var version = "dev"

func main() {
	cli.Execute(version)
}
