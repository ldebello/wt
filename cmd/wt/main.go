package main

import (
	"os"

	"github.com/ldebello/wt/internal/cli"
)

// version is set at build time: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	os.Exit(cli.Execute(version))
}
