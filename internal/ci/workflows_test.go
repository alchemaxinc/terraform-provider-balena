// Package ci tests the GitHub workflows and their scripts.
package ci

import (
	"os"
	"regexp"
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

type fullJob struct {
	Environment any `yaml:"environment"`
	Steps       []struct {
		Uses string `yaml:"uses"`
		Run  string `yaml:"run"`
	} `yaml:"steps"`
}

// PRIVILEGED_USES are the only actions a job holding the GitHub App key may use: none of them builds or runs the
// repository's code.
var PRIVILEGED_USES = map[string]bool{
	"actions/create-github-app-token@v3":                  true,
	"actions/checkout@v7":                                 true,
	"actions/download-artifact@v8":                        true,
	"alchemaxinc/composite-toolbox/checkout-and-setup@v1": true,
	"alchemaxinc/composite-toolbox/check-existing-pr@v1":  true,
	"alchemaxinc/composite-toolbox/create-pr@v1":          true,
	"alchemaxinc/composite-toolbox/merge-pr@v1":           true,
}

// TestPrivilegedJobsRunNoGeneratedCode checks the workflows that publish generated files: the GitHub App key
// is only read in the `housekeeping` environment, and a job that reads it never builds or runs code from the
// repository or from upstream. It runs git, gh, the composite actions above and only the helpers of
// schema-update.sh that validate text.
func TestPrivilegedJobsRunNoGeneratedCode(t *testing.T) {
	forbidden := regexp.MustCompile(`(^|[\s;&|(])(go|make|npm|npx|node|terraform|tfplugindocs)\s`)
	helper := regexp.MustCompile(`schema-update\.sh\s+([a-z-]+)`)
	safeHelpers := map[string]bool{"title": true, "branch-safe": true, "generated-paths": true}
	for _, name := range []string{"check-schema.yml", "update-provider-version-examples.yml"} {
		data, err := os.ReadFile(WORKFLOWS + name)
		if err != nil {
			t.Fatal(err)
		}
		var w struct {
			Jobs map[string]*fullJob `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(data, &w); err != nil {
			t.Fatal(err)
		}
		privileged := 0
		for jobName, j := range w.Jobs {
			if !strings.Contains(jobSource(t, string(data), jobName), "HOUSEKEEPING_BOT_PRIVATE_KEY") {
				if j.Environment == "housekeeping" {
					t.Errorf("%s: %s is in housekeeping without needing the key", name, jobName)
				}
				continue
			}
			privileged++
			if j.Environment != "housekeeping" {
				t.Errorf("%s: %s reads the App key outside the housekeeping environment", name, jobName)
			}
			for _, s := range j.Steps {
				if s.Uses != "" && !PRIVILEGED_USES[s.Uses] {
					t.Errorf("%s: %s uses %s while holding the App key", name, jobName, s.Uses)
				}
				if m := forbidden.FindString(s.Run); m != "" {
					t.Errorf("%s: %s runs %q while holding the App key", name, jobName, strings.TrimSpace(m))
				}
				for _, m := range helper.FindAllStringSubmatch(s.Run, -1) {
					if !safeHelpers[m[1]] {
						t.Errorf("%s: %s runs schema-update.sh %s while holding the App key", name, jobName, m[1])
					}
				}
			}
		}
		if privileged != 1 {
			t.Errorf("%s: %d jobs read the App key, want exactly the publish job", name, privileged)
		}
	}
}

// jobSource returns the text of one job of a workflow file.
func jobSource(t *testing.T, workflow, name string) string {
	t.Helper()
	start := strings.Index(workflow, "\n  "+name+":\n")
	if start < 0 {
		t.Fatalf("no job %s", name)
	}
	rest := workflow[start+1:]
	if end := regexp.MustCompile(`\n  [a-z][a-z0-9_-]*:\n`).FindStringIndex(rest[1:]); end != nil {
		return rest[:end[0]+1]
	}
	return rest
}
