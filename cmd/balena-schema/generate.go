package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/alchemaxinc/terraform-provider-balena/internal/gen"
)

func init() {
	COMMANDS["generate"] = cmdGenerate
	COMMANDS["check"] = cmdCheck
	COMMANDS["ir"] = cmdIR
}

func cmdGenerate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	res, err := gen.Generate(*repo)
	if err != nil {
		return err
	}
	stale := res.Stale(*repo)
	if err := res.Write(*repo); err != nil {
		return err
	}
	if len(stale) == 0 {
		_, _ = fmt.Fprintln(stdout, "generated files are up to date")
	} else {
		_, _ = fmt.Fprintln(stdout, "updated "+strings.Join(stale, ", "))
	}
	return nil
}

// cmdCheck runs every offline gate: the lock verifies, the sources cross-check, every table is classified, the
// overrides are valid, and generating again changes no committed file.
func cmdCheck(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	res, err := gen.Generate(*repo)
	if err != nil {
		return err
	}
	if stale := res.Stale(*repo); len(stale) > 0 {
		return fmt.Errorf("generated files are stale; run `make generate` and commit: %s", strings.Join(stale, ", "))
	}
	counts := map[string]int{}
	for _, t := range res.IR.Tables {
		counts[t.Class.Status]++
	}
	_, _ = fmt.Fprintf(stdout, "ok: lock verified, sources cross-checked, %d tables classified (%d generated, %d handwritten, %d excluded, %d unscoped), generated files up to date\n",
		len(res.IR.Tables), counts[gen.STATUS_GENERATED], counts[gen.STATUS_HANDWRITTEN], counts[gen.STATUS_EXCLUDED], counts[gen.STATUS_UNSCOPED])
	return nil
}

// cmdIR prints the IR built for a preference of roots, for example `ir -root v8` before promoting v8.
func cmdIR(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ir", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root")
	root := fs.String("root", "", "API root to build the IR for (default: the locked default root); tables it lacks fall back to /resin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ir, err := buildIR(*repo, *root)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(ir, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(data))
	return err
}

// buildIR builds and classifies the IR for a root, with the specs of the in-scope tables.
func buildIR(repo, root string) (*gen.IR, error) {
	res, err := gen.GenerateForRoot(repo, root)
	if err != nil {
		return nil, err
	}
	return res.IR, nil
}
