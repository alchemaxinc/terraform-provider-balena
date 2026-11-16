package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"regexp"

	"github.com/alchemaxinc/terraform-provider-balena/internal/gen"
)

func init() {
	COMMANDS["surface-diff"] = cmdSurfaceDiff
}

// COMMIT_TYPE_RE reads the type of a conventional commit title: "feat!: ...", "fix(scope): ...".
var COMMIT_TYPE_RE = regexp.MustCompile(`^([a-z]+)(\([^)]*\))?(!)?: `)

// commitLevel maps a conventional commit title to the largest surface change it may carry.
func commitLevel(title string) (string, error) {
	m := COMMIT_TYPE_RE.FindStringSubmatch(title)
	if m == nil {
		return "", fmt.Errorf("%q is not a conventional commit title", title)
	}
	switch {
	case m[3] == "!":
		return gen.LEVEL_BREAKING, nil
	case m[1] == "feat":
		return gen.LEVEL_FEAT, nil
	case m[1] == "fix":
		return gen.LEVEL_FIX, nil
	}
	return gen.LEVEL_NONE, nil
}

// cmdSurfaceDiff compares two Terraform surfaces: two surface lock files, or the surfaces two API roots would give.
// With -title it fails when the change needs a bigger conventional commit type than the title has.
func cmdSurfaceDiff(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("surface-diff", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root")
	oldPath := fs.String("old", "", "old surface lock file")
	newPath := fs.String("new", "", "new surface lock file (default: generate from the repository)")
	oldRoot := fs.String("old-root", "", "API root whose generated surface is the old side")
	newRoot := fs.String("new-root", "", "API root whose generated surface is the new side")
	title := fs.String("title", "", "conventional commit title the change ships under; fails when it is too small")
	allowMissing := fs.Bool("allow-missing-old", false, "treat a missing -old file as an empty surface")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	load := func(path, root string, allowEmpty bool) (*gen.SurfaceLock, error) {
		if path != "" {
			if !allowEmpty {
				if _, err := readFile(path); err != nil {
					return nil, err
				}
			}
			return gen.ReadSurfaceLock(path)
		}
		res, err := gen.GenerateForRoot(*repo, root)
		if err != nil {
			return nil, err
		}
		return res.Surface, nil
	}
	if *oldPath == "" && *oldRoot == "" {
		return fmt.Errorf("pass -old or -old-root")
	}
	old, err := load(*oldPath, *oldRoot, *allowMissing)
	if err != nil {
		return err
	}
	cur, err := load(*newPath, *newRoot, false)
	if err != nil {
		return err
	}
	changes, level := gen.SurfaceDiff(old, cur)
	if *asJSON {
		data, err := json.MarshalIndent(map[string]any{"level": level, "changes": changes}, "", "  ")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, string(data))
	} else {
		for _, c := range changes {
			_, _ = fmt.Fprintf(stdout, "%-5s %s\n", c.Level, c.Message)
		}
		_, _ = fmt.Fprintf(stdout, "level: %s (%d changes)\n", level, len(changes))
	}
	if *title != "" {
		allowed, err := commitLevel(*title)
		if err != nil {
			return err
		}
		if gen.LEVEL_ORDER[level] > gen.LEVEL_ORDER[allowed] {
			return fmt.Errorf("the surface change needs a %q commit, but the title %q allows at most %q", level, *title, allowed)
		}
	}
	return nil
}
