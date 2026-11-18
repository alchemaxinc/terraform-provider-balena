// Package ci tests the GitHub workflows and their scripts.
package ci

import (
	"os"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const WORKFLOWS = "../../.github/workflows/"

type workflow struct {
	On          map[string]any         `yaml:"on"`
	Concurrency struct{ Group string } `yaml:"concurrency"`
	Jobs        map[string]*job        `yaml:"jobs"`
}

type job struct {
	If string `yaml:"if"`
}

func readWorkflow(t *testing.T, name string) *workflow {
	t.Helper()
	data, err := os.ReadFile(WORKFLOWS + name)
	if err != nil {
		t.Fatal(err)
	}
	var w workflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return &w
}

func TestTitleAndBodyEditsTriggerTheTitleChecks(t *testing.T) {
	for _, name := range []string{"test.yml", "lint.yml"} {
		w := readWorkflow(t, name)
		pr, _ := w.On["pull_request"].(map[string]any)
		types, _ := pr["types"].([]any)
		var got []string
		for _, ty := range types {
			got = append(got, ty.(string))
		}
		sort.Strings(got)
		if strings.Join(got, ",") != "edited,opened,reopened,synchronize" {
			t.Errorf("%s: pull_request types = %v, want the defaults plus edited", name, got)
		}
	}
}

// LIVE_GATE is the condition of the live API jobs: they run unless the title is a plain chore (`chore:` or
// `chore(scope):`). A breaking `chore!:` or `chore(scope)!:` runs them, since a breaking change must be tested live.
const LIVE_GATE = "!(startsWith(github.event.pull_request.title, 'chore:') " +
	"|| (startsWith(github.event.pull_request.title, 'chore(') " +
	"&& !contains(github.event.pull_request.title, ')!:')))"

func TestLiveJobsSkipOnlyPlainChoreTitles(t *testing.T) {
	w := readWorkflow(t, "test.yml")
	for _, name := range []string{"contract", "acceptance-test"} {
		j := w.Jobs[name]
		if j == nil {
			t.Fatalf("test.yml has no %s job", name)
		}
		if got := strings.Join(strings.Fields(j.If), " "); got != LIVE_GATE {
			t.Errorf("%s: if = %q, want %q", name, got, LIVE_GATE)
		}
	}
}

// TestEditsRunEveryJob checks that an edit is an ordinary run: no job depends on the event action, and runs of a
// pull request share one concurrency group, so a newer run cancels the older one.
func TestEditsRunEveryJob(t *testing.T) {
	for _, name := range []string{"test.yml", "lint.yml"} {
		w := readWorkflow(t, name)
		if w.Concurrency.Group != "${{ github.workflow }}-${{ github.ref }}" {
			t.Errorf("%s: concurrency group = %q", name, w.Concurrency.Group)
		}
		for jobName, j := range w.Jobs {
			if strings.Contains(j.If, "github.event.action") || strings.Contains(j.If, "github.event.changes") {
				t.Errorf("%s: %s depends on the kind of event: %s", name, jobName, j.If)
			}
		}
	}
}

// TestPromotionTextIsSanitised checks that the promotion plan, which quotes upstream text, is sanitised for Markdown
// wherever check-schema.yml renders it.
func TestPromotionTextIsSanitised(t *testing.T) {
	data, err := os.ReadFile(WORKFLOWS + "check-schema.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	uses := 0
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "promotion.md") && !strings.Contains(line, "$SUMMARY") {
			continue
		}
		switch {
		case strings.Contains(line, "promote-root"), strings.Contains(line, `SUMMARY="$RUNNER_TEMP/promotion.md"`):
			// Writes the plan, or names its file.
		case strings.Contains(line, "| bash .github/scripts/schema-update.sh sanitize-markdown"):
			uses++
		default:
			t.Errorf("the promotion plan is used without sanitize-markdown: %s", strings.TrimSpace(line))
		}
	}
	if uses != 2 {
		t.Errorf("the promotion plan is rendered %d times through sanitize-markdown, want 2 (job summary, pull request)", uses)
	}
}
