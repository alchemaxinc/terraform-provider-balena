package gen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/alchemaxinc/terraform-provider-balena/internal/gen/sources"
)

// Promotion statuses.
const (
	// PROMOTION_NONE means no locked root is newer than the default root.
	PROMOTION_NONE = "none"
	// PROMOTION_READY means the newer root builds and passes its contract, so it can become the default root.
	PROMOTION_READY = "ready"
	// PROMOTION_BLOCKED means the newer root cannot become the default root yet; the current root stays.
	PROMOTION_BLOCKED = "blocked"
	// PROMOTION_DONE means ApplyPromotion made the newer root the default root.
	PROMOTION_DONE = "promoted"
)

// ContractRunner runs the live contract checks for a generation result and returns them with their status set.
type ContractRunner func(res *Result) ([]*ContractCheck, error)

// PromotionPlan is what promoting a newer API root to the default root would do.
type PromotionPlan struct {
	Current   string `json:"current"`
	Candidate string `json:"candidate,omitempty"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	// Level is the conventional commit type the surface change needs.
	Level   string   `json:"level,omitempty"`
	Changes []Change `json:"changes,omitempty"`
	// Fallbacks lists in-scope tables the candidate root lacks, with the root that serves them instead.
	Fallbacks        []string `json:"fallbacks,omitempty"`
	ContractFailures []string `json:"contract_failures,omitempty"`
	// CurrentError is set when the current default root no longer generates.
	CurrentError string `json:"current_error,omitempty"`
}

// rootVersion returns N for a versioned root "vN", or 0.
func rootVersion(name string) int {
	if !strings.HasPrefix(name, "v") {
		return 0
	}
	n, err := strconv.Atoi(name[1:])
	if err != nil {
		return 0
	}
	return n
}

// NewestRoot returns the newest versioned locked root.
func NewestRoot(lock *sources.Lock) string {
	newest := ""
	for _, r := range lock.Roots {
		if rootVersion(r.Name) > rootVersion(newest) {
			newest = r.Name
		}
	}
	return newest
}

// PlanPromotion decides whether a newer locked root can become the default root. The candidate is the given root, or
// the newest versioned root when it is newer than the default root. The candidate must build (cross-check, gap check,
// overrides and specs, with every in-scope table it lacks served by the unversioned root) and pass its live contract;
// the surface diff against the current root then gives the commit type. A blocked plan leaves the current root in
// place. PlanPromotion changes no file.
func PlanPromotion(repoDir, candidate string, contract ContractRunner) (*PromotionPlan, error) {
	lock, err := sources.ReadLock(repoDir)
	if err != nil {
		return nil, err
	}
	plan := &PromotionPlan{Current: lock.DefaultRoot}
	if candidate == "" {
		candidate = NewestRoot(lock)
		if rootVersion(candidate) <= rootVersion(lock.DefaultRoot) {
			plan.Status = PROMOTION_NONE
			plan.Reason = "no locked root is newer than /" + lock.DefaultRoot
			return plan, nil
		}
	}
	plan.Candidate = candidate
	if candidate == lock.DefaultRoot {
		plan.Status = PROMOTION_NONE
		plan.Reason = "/" + candidate + " is already the default root"
		return plan, nil
	}
	if r, ok := lock.Root(candidate); !ok || rootVersion(r.Name) == 0 {
		return nil, fmt.Errorf("%q is not a locked versioned root", candidate)
	}
	// The surface diff is against the current root's generation; when the current root no longer generates (upstream
	// broke or retired it), against the committed surface.
	var currentSurface *SurfaceLock
	if current, err := Generate(repoDir); err == nil {
		currentSurface = current.Surface
	} else {
		plan.CurrentError = err.Error()
		if currentSurface, err = readRepoSurfaceLock(repoDir); err != nil {
			return nil, err
		}
	}
	next, err := GenerateForRoot(repoDir, candidate)
	if err != nil {
		plan.Status = PROMOTION_BLOCKED
		plan.Reason = "the generator cannot build /" + candidate + ": " + err.Error()
		return plan, nil
	}
	for _, s := range next.Specs {
		if s.Root != candidate {
			plan.Fallbacks = append(plan.Fallbacks, s.Table+" (served from /"+s.Root+")")
		}
	}
	sort.Strings(plan.Fallbacks)
	plan.Changes, plan.Level = SurfaceDiff(currentSurface, next.Surface)
	checks, err := contract(next)
	if err != nil {
		plan.Status = PROMOTION_BLOCKED
		plan.Reason = "the live contract of /" + candidate + " could not run: " + err.Error()
		return plan, nil
	}
	if failures := ContractFailures(checks); len(failures) > 0 {
		plan.Status = PROMOTION_BLOCKED
		plan.ContractFailures = failures
		plan.Reason = fmt.Sprintf("%d of %d live contract checks of /%s fail", len(failures), len(checks), candidate)
		return plan, nil
	}
	plan.Status = PROMOTION_READY
	plan.Reason = "/" + candidate + " builds and passes its live contract"
	return plan, nil
}

// ApplyPromotion makes a ready plan's candidate the default root in the lock and regenerates every file.
func ApplyPromotion(repoDir string, plan *PromotionPlan) error {
	if plan.Status != PROMOTION_READY {
		return fmt.Errorf("promotion of %q is %s, not %s", plan.Candidate, plan.Status, PROMOTION_READY)
	}
	lock, err := sources.ReadLock(repoDir)
	if err != nil {
		return err
	}
	if lock.DefaultRoot != plan.Current {
		return fmt.Errorf("the default root changed from %s to %s since the plan was made", plan.Current, lock.DefaultRoot)
	}
	lock.DefaultRoot = plan.Candidate
	if err := lock.Validate(); err != nil {
		return err
	}
	data, err := lock.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(repoDir, sources.LOCK_PATH), data, 0o644); err != nil {
		return err
	}
	res, err := Generate(repoDir)
	if err != nil {
		return err
	}
	if err := res.Write(repoDir); err != nil {
		return err
	}
	plan.Status = PROMOTION_DONE
	return nil
}

// Markdown renders the plan for a job summary or a pull request body.
func (p *PromotionPlan) Markdown() string {
	var b strings.Builder
	switch p.Status {
	case PROMOTION_NONE:
		fmt.Fprintf(&b, "API root: `/%s` stays the default root (%s).\n", p.Current, p.Reason)
		return b.String()
	case PROMOTION_DONE:
		fmt.Fprintf(&b, "API root: promoted `/%s` to the default root, replacing `/%s`. Surface change: `%s`.\n", p.Candidate, p.Current, p.Level)
	case PROMOTION_READY:
		fmt.Fprintf(&b, "API root: `/%s` can replace `/%s` as the default root. Surface change: `%s`.\n", p.Candidate, p.Current, p.Level)
	default:
		fmt.Fprintf(&b, "API root: `/%s` was **not** promoted; `/%s` stays the default root. %s.\n", p.Candidate, p.Current, p.Reason)
	}
	if p.CurrentError != "" {
		fmt.Fprintf(&b, "- the current root `/%s` no longer generates: %s\n", p.Current, p.CurrentError)
	}
	for _, f := range p.Fallbacks {
		fmt.Fprintf(&b, "- not in `/%s`: %s\n", p.Candidate, f)
	}
	for _, f := range p.ContractFailures {
		fmt.Fprintf(&b, "- contract: %s\n", f)
	}
	for _, c := range p.Changes {
		fmt.Fprintf(&b, "- `%s` %s\n", c.Level, c.Message)
	}
	return b.String()
}
