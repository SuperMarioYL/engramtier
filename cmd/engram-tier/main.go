// Package main is the engram-tier binary entry point. It does nothing beyond
// delegating to internal/cli so all command logic is testable without the
// os.Args plumbing living in main.
package main

import (
	"fmt"
	"os"

	"github.com/SuperMarioYL/engramtier/internal/cli"
)

// Version overrides cli.DefaultVersion when set at link time by the release
// workflow (-ldflags "-X main.Version=..."); empty in plain builds, where
// the CLI reports cli.DefaultVersion.
var Version = ""

func main() {
	root := cli.NewRoot()
	if Version != "" {
		root.Version = Version
	}
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
