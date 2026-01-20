// Package main provides the shadow CLI for validating GitOps structure
package main

import (
	"os"

	"github.com/erauner/homelab-shadow/cmd/shadow/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
