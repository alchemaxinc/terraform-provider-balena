package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/alchemaxinc/terraform-provider-balena/internal/gen"
)

func init() {
	COMMANDS["promote-root"] = cmdPromoteRoot
}

// cmdPromoteRoot plans, and with -apply performs, the promotion of the newest locked API root to the default root.
// A root that does not build or fails its live contract is not promoted: the plan says so and the command still
// succeeds, so a scheduled run can go on with the current root. Only an error of the command itself fails it.
func cmdPromoteRoot(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("promote-root", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root")
	root := fs.String("root", "", "root to promote (default: the newest locked root, when it is newer than the default root)")
	apply := fs.Bool("apply", false, "make the root the default root and regenerate when it is ready")
	apiURL := fs.String("api-url", os.Getenv("BALENA_API_URL"), "API URL (default: BALENA_API_URL or balena-cloud)")
	asJSON := fs.Bool("json", false, "print the plan as JSON instead of Markdown")
	markdown := fs.String("markdown", "", "also append the plan as Markdown to this file (for a job summary)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	token := os.Getenv("BALENA_API_TOKEN")
	runner := func(res *gen.Result) ([]*gen.ContractCheck, error) {
		if token == "" {
			return nil, fmt.Errorf("BALENA_API_TOKEN must be set")
		}
		client := balena.NewClient(*apiURL, token, "contract", balena.WithTimeout(60*time.Second))
		ctx := context.Background()
		if _, err := client.Do(ctx, "GET", "/actor/v1/whoami", nil); err != nil {
			return nil, fmt.Errorf("BALENA_API_TOKEN does not authenticate: %w", err)
		}
		checks := gen.ContractChecks(res.IR, res.Specs, res.Overrides)
		gen.RunContract(ctx, client, checks)
		return checks, nil
	}
	plan, err := gen.PlanPromotion(*repo, *root, runner)
	if err != nil {
		return err
	}
	if *apply && plan.Status == gen.PROMOTION_READY {
		if err := gen.ApplyPromotion(*repo, plan); err != nil {
			return err
		}
	}
	if *markdown != "" {
		f, err := os.OpenFile(*markdown, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, werr := f.WriteString(plan.Markdown())
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return werr
		}
	}
	if *asJSON {
		data, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, string(data))
		return err
	}
	_, err = fmt.Fprint(stdout, plan.Markdown())
	return err
}
