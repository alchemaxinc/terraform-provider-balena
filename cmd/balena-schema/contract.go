package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/alchemaxinc/terraform-provider-balena/internal/gen"
)

func init() {
	COMMANDS["contract"] = cmdContract
}

// cmdContract checks the specs against the live API with read-only GET requests. The token comes from
// BALENA_API_TOKEN and is never printed.
func cmdContract(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("contract", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root")
	root := fs.String("root", "", "API root to build the specs for (default: the locked default root)")
	apiURL := fs.String("api-url", os.Getenv("BALENA_API_URL"), "API URL (default: BALENA_API_URL or balena-cloud)")
	all := fs.Bool("all", false, "also probe every table outside the scope with every column (report only: never fails)")
	asJSON := fs.Bool("json", false, "print the results as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	token := os.Getenv("BALENA_API_TOKEN")
	if token == "" {
		return fmt.Errorf("BALENA_API_TOKEN must be set")
	}
	res, err := gen.GenerateForRoot(*repo, *root)
	if err != nil {
		return err
	}
	client := balena.NewClient(*apiURL, token, "contract", balena.WithTimeout(60*time.Second))
	ctx := context.Background()
	if _, err := client.Do(ctx, "GET", "/actor/v1/whoami", nil); err != nil {
		return fmt.Errorf("BALENA_API_TOKEN does not authenticate: %w", err)
	}
	checks := gen.ContractChecks(res.IR, res.Specs, res.Overrides)
	gen.RunContract(ctx, client, checks)
	var extra []*gen.ContractCheck
	if *all {
		extra = gen.AllTableChecks(res.IR)
		gen.RunContract(ctx, client, extra)
	}
	if *asJSON {
		data, err := json.MarshalIndent(map[string]any{"checks": checks, "all_tables": extra}, "", "  ")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, string(data))
	} else {
		for _, c := range append(append([]*gen.ContractCheck{}, checks...), extra...) {
			_, _ = fmt.Fprintf(stdout, "%-4s %-60s /%s/%s: %s\n", c.Status, c.What, c.Root, c.Table, c.Detail)
		}
	}
	if failures := gen.ContractFailures(checks); len(failures) > 0 {
		return fmt.Errorf("%d of %d contract checks failed:\n%s", len(failures), len(checks), strings.Join(failures, "\n"))
	}
	_, _ = fmt.Fprintf(stdout, "ok: %d contract checks passed\n", len(checks))
	return nil
}
