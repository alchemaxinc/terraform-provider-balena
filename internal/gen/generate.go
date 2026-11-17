package gen

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Result is everything one generation produces.
type Result struct {
	IR        *IR
	Specs     []*Spec
	Surface   *SurfaceLock
	Overrides *Overrides
	// Files maps repository-relative paths to their generated content.
	Files map[string][]byte
	// orphans are generated files no spec produces any more.
	orphans []string
}

// Generate builds the IR from the pinned sources and overrides and renders every generated file. It reads only the
// repository: no network. Any cross-check, gap or override problem is an error.
func Generate(repoDir string) (*Result, error) {
	return GenerateForRoot(repoDir, "")
}

// GenerateForRoot is Generate with the given API root preferred over the locked default root (tables the root lacks
// fall back to the unversioned root). It is how a new root (v8) is evaluated before it is promoted.
func GenerateForRoot(repoDir, root string) (*Result, error) {
	return GenerateWith(repoDir, Options{Root: root})
}

// Options adjust a generation.
type Options struct {
	// Root is preferred over the locked default root when set.
	Root string
	// FreshNames ignores the committed surface lock when naming, so the current rules (and the names map) name every
	// attribute. Only an intentional rename uses it; the surface diff then shows every renamed attribute.
	FreshNames bool
}

// GenerateWith is Generate with options.
func GenerateWith(repoDir string, opts Options) (*Result, error) {
	root := opts.Root
	in, err := LoadInputs(repoDir)
	if err != nil {
		return nil, err
	}
	o, err := LoadOverrides(repoDir)
	if err != nil {
		return nil, err
	}
	var preference []string
	if root != "" {
		if _, ok := in.Lock.Root(root); !ok {
			return nil, fmt.Errorf("root %q is not in %s", root, "schema/sources.lock.json")
		}
		preference = []string{root, "resin"}
	}
	ir, problems, err := BuildIR(in, preference)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("sources disagree:\n%s", strings.Join(problems, "\n"))
	}
	preview := map[string]bool{}
	for _, r := range in.Lock.Roots {
		preview[r.Name] = r.Preview
	}
	if err := Classify(ir, o, preview); err != nil {
		return nil, err
	}
	if gaps := GapCheck(ir); len(gaps) > 0 {
		return nil, fmt.Errorf("gap check failed:\n%s", strings.Join(gaps, "\n"))
	}
	hooks, err := implementedHooks(repoDir)
	if err != nil {
		return nil, err
	}
	old, err := readRepoSurfaceLock(repoDir)
	if err != nil {
		return nil, err
	}
	if opts.FreshNames {
		old = &SurfaceLock{Format: SURFACE_FORMAT}
	}
	specs, err := BuildSpecs(ir, o, in.ConfigVars, old, hooks)
	if err != nil {
		return nil, err
	}
	surface := BuildSurface(specs)
	res := &Result{IR: ir, Specs: specs, Surface: surface, Overrides: o, Files: map[string][]byte{}}
	if res.Files[MODEL_PATH], err = renderModel(ir); err != nil {
		return nil, err
	}
	if res.Files[SURFACE_LOCK_PATH], err = surface.Marshal(); err != nil {
		return nil, err
	}
	naming, _ := NamingPreview(ir, o, old)
	fresh, _ := NamingPreview(ir, o, nil)
	res.Files[COVERAGE_PATH] = renderCoverage(ir, specs, Collisions(naming), StickyDrifts(naming, fresh))
	if res.Files[SPECS_PATH], err = renderGoSpecs(specs, o); err != nil {
		return nil, err
	}
	for p, data := range renderExamples(specs) {
		res.Files[p] = data
	}
	res.orphans = orphanExamples(repoDir, res.Files)
	return res, nil
}

// Write writes the generated files and removes orphaned ones.
func (r *Result) Write(repoDir string) error {
	for _, p := range r.orphans {
		full := filepath.Join(repoDir, filepath.FromSlash(p))
		if err := os.Remove(full); err != nil {
			return err
		}
		_ = os.Remove(filepath.Dir(full))
	}
	for _, p := range r.paths() {
		full := filepath.Join(repoDir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, r.Files[p], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Stale lists the generated files whose committed content differs from the generated content.
func (r *Result) Stale(repoDir string) []string {
	var stale []string
	for _, p := range r.paths() {
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(p)))
		if err != nil || !bytes.Equal(data, r.Files[p]) {
			stale = append(stale, p)
		}
	}
	for _, p := range r.orphans {
		stale = append(stale, p+" (orphan)")
	}
	return stale
}

func (r *Result) paths() []string {
	out := make([]string, 0, len(r.Files))
	for p := range r.Files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
