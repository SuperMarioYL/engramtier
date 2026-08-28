// Package main is the engram-tier binary entry point. It does nothing beyond
// delegating to internal/cli so all command logic is testable without the
// os.Args plumbing living in main.
package main

import (
	"fmt"
	"os"

	"github.com/SuperMarioYL/engramtier/internal/cli"
)

// Version is the build version. Overridden at link time with
// -ldflags "-X main.Version=..." by the release workflow; defaults to the
// VERSION file contents read by the release tooling.
var Version = "0.1.0"

func main() {
	root := cli.NewRoot()
	root.Version = Version
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
