// Command balena-schema pins the upstream balena models, builds the provider's intermediate representation (IR) from
// them, and generates the provider's resource specs, docs inputs and reports.
//
// Subcommands:
//
//	fetch         download the pinned upstream files and rewrite schema/sources.lock.json (network)
//	generate      write schema/model.json, schema/surface.lock.json, schema/coverage.md and the Go specs (offline)
//	check         verify the lock, the cross-checks and the gap check, and that generate would change nothing (offline)
//	ir            print the IR for any locked API root (offline)
//	surface-diff  compare the Terraform surface of two surface locks or two API roots (offline)
//	contract      check every spec's table and $select-able fields against the live API (read-only, network)
package main

import (
	"fmt"
	"io"
	"os"
)

// COMMANDS maps a subcommand name to its implementation; each subcommand registers itself from its own file.
var COMMANDS = map[string]func(args []string, stdout io.Writer) error{}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "usage: balena-schema <fetch|generate|check|ir|surface-diff|contract> [flags]")
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		usage(stdout)
		return 0
	}
	cmd, ok := COMMANDS[args[0]]
	if !ok {
		usage(stderr)
		return 2
	}
	if err := cmd(args[1:], stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "balena-schema "+args[0]+": "+err.Error())
		return 1
	}
	return 0
}
