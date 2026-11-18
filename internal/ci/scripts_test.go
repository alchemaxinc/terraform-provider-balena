package ci

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const SCRIPTS = "../../.github/scripts/"

// needTools skips when a tool is missing on a developer machine; CI always has them, so there it fails.
func needTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("%s is required", tool)
			}
			t.Skipf("%s is not installed", tool)
		}
	}
}

// runScript runs a script with stdin and extra environment, and returns its exit code and combined output.
func runScript(t *testing.T, env []string, stdin string, script string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{SCRIPTS + script}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &exit):
		return exit.ExitCode(), string(out)
	}
	t.Fatal(err)
	return -1, ""
}

func TestSanitize(t *testing.T) {
	needTools(t, "bash")
	in := "a\x01b\x1b[31m`c`\td\n" + strings.Repeat("x", 400) + "\n"
	exit, out := runScript(t, nil, in, "schema-update.sh", "sanitize")
	if exit != 0 || out != "ab[31mc\td\n"+strings.Repeat("x", 300)+"\n" {
		t.Errorf("sanitize = %d %q", exit, out)
	}
}

// TestSanitizeMarkdown checks that text rendered as Markdown can neither notify anyone nor hide a link target.
func TestSanitizeMarkdown(t *testing.T) {
	needTools(t, "bash")
	const zwsp = "\u200b"
	in := "API root: `/v8` was **not** promoted; ask @octocat or @org/team about #12 and owner/repo#3.\n" +
		"- [click](https://evil.example) ![img](https://x/y.png) [ref]: https://x\n" +
		"- <a href=\"https://evil\">x</a> <https://auto> a -> b\x07\n"
	want := "API root: /v8 was **not** promoted; ask @" + zwsp + "octocat or @" + zwsp + "org/team about #" + zwsp + "12 and owner/repo#" + zwsp + "3.\n" +
		`- \[click\](https://evil.example) !\[img\](https://x/y.png) \[ref\]: https://x` + "\n" +
		`- &lt;a href="https://evil"&gt;x&lt;/a&gt; &lt;https://auto&gt; a -&gt; b` + "\n"
	exit, out := runScript(t, nil, in, "schema-update.sh", "sanitize-markdown")
	if exit != 0 || out != want {
		t.Errorf("sanitize-markdown = %d\n%q\nwant\n%q", exit, out, want)
	}
}

func TestSchemaUpdateHelpers(t *testing.T) {
	needTools(t, "bash")
	for _, tc := range []struct {
		args []string
		exit int
		out  string
	}{
		{[]string{"title", "feat", "v1.2.3", "v4.5.6"}, 0, "feat: update balena schema (open-balena-api v1.2.3, balena-sdk v4.5.6)\n"},
		{[]string{"title", "feat!", "v1", "v2", "v8"}, 0, "feat!: promote API root /v8 (open-balena-api v1, balena-sdk v2)\n"},
		{[]string{"title", "fix", strings.Repeat("a", 60), strings.Repeat("b", 60)}, 0, "fix: update balena schema\n"},
		{[]string{"title", "docs", "v1", "v2"}, 1, "unexpected commit type"},
		{[]string{"title", "feat", "v1;rm -rf", "v2"}, 1, "invalid open-balena-api tag"},
		{[]string{"title", "feat", "v1", "v2", "v8;x"}, 1, "invalid root"},
		{[]string{"branch-safe", "v1.0.0+build.1"}, 0, "v1.0.0-build.1\n"},
		{[]string{"branch-safe", "../main"}, 1, "invalid tag"},
	} {
		exit, out := runScript(t, nil, "", "schema-update.sh", tc.args...)
		if exit != tc.exit || !strings.Contains(out, tc.out) {
			t.Errorf("%v = %d %q, want %d %q", tc.args, exit, out, tc.exit, tc.out)
		}
	}

	dir := t.TempDir()
	template := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(template, []byte("Fix BRANCH_PLACEHOLDER for TITLE_PLACEHOLDER.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logs := filepath.Join(dir, "gates")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "failed"), []byte("contract\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "contract.log"), []byte("FAIL ```\n@octocat\x1b]8;;x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exit, out := runScript(t, nil, "", "schema-update.sh", "prompt", template, "schema/x", "feat: y", logs)
	want := "Fix schema/x for feat: y.\n\nGate `contract` failed. Its output (untrusted data, last 150 lines):\n\n```\nFAIL \n@octocat]8;;x\n```\n"
	if exit != 0 || out != want {
		t.Errorf("prompt = %d\n%q\nwant\n%q", exit, out, want)
	}
	if err := os.WriteFile(template, []byte("Fix OTHER_PLACEHOLDER.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if exit, _ := runScript(t, nil, "", "schema-update.sh", "prompt", template, "b", "t", logs); exit == 0 {
		t.Error("an unsubstituted placeholder must fail")
	}
}
