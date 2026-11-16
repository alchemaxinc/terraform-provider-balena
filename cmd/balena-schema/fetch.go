package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/alchemaxinc/terraform-provider-balena/internal/gen/sources"
)

func init() {
	COMMANDS["fetch"] = cmdFetch
}

func cmdFetch(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root")
	oba := fs.String("open-balena-api", "", "open-balena-api tag (default: the locked tag)")
	sdk := fs.String("balena-sdk", "", "balena-sdk tag (default: the locked tag)")
	latest := fs.Bool("latest", false, "pin the newest tag of every git source")
	defaultRoot := fs.String("default-root", "", "API root the provider defaults to (default: the locked default)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	lock, err := sources.Fetch(context.Background(), sources.FetchOptions{
		RepoDir:          *repo,
		OpenBalenaAPITag: *oba,
		BalenaSDKTag:     *sdk,
		Latest:           *latest,
		DefaultRoot:      *defaultRoot,
		GitHubToken:      os.Getenv("GITHUB_TOKEN"),
		Log:              stdout,
	})
	if err != nil {
		return err
	}
	var roots []string
	for _, r := range lock.Roots {
		roots = append(roots, r.Name)
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s: roots %v, default %s\n", sources.LOCK_PATH, roots, lock.DefaultRoot)
	return nil
}
