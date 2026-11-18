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

// TestApplyArtifact covers the copy into a privileged checkout: only regular files under the allowed paths are
// accepted, a bad artifact changes nothing, and an allowed directory is mirrored so that deletions arrive too.
func TestApplyArtifact(t *testing.T) {
	needTools(t, "bash", "find")
	write := func(t *testing.T, path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			return "<missing>"
		}
		return string(data)
	}
	allowed := []string{"schema", "docs", "internal/provider/zz_generated_specs.go"}
	setup := func(t *testing.T) (string, string) {
		t.Helper()
		dir := t.TempDir()
		src, dest := filepath.Join(dir, "artifact"), filepath.Join(dir, "checkout")
		write(t, filepath.Join(src, "schema/model.json"), "new model")
		write(t, filepath.Join(src, "docs/resources/a.md"), "new a")
		write(t, filepath.Join(src, "internal/provider/zz_generated_specs.go"), "new specs")
		write(t, filepath.Join(dest, "schema/model.json"), "old model")
		write(t, filepath.Join(dest, "docs/resources/removed.md"), "removed")
		write(t, filepath.Join(dest, "internal/provider/zz_generated_specs.go"), "old specs")
		write(t, filepath.Join(dest, "internal/provider/hooks.go"), "hooks")
		write(t, filepath.Join(dest, ".github/workflows/x.yml"), "workflow")
		return src, dest
	}
	apply := func(t *testing.T, src, dest string) (int, string) {
		t.Helper()
		return runScript(t, nil, "", "apply-artifact.sh", append([]string{src, dest}, allowed...)...)
	}
	unchanged := func(t *testing.T, dest string) {
		t.Helper()
		if read(filepath.Join(dest, "schema/model.json")) != "old model" || read(filepath.Join(dest, "docs/resources/removed.md")) != "removed" {
			t.Error("a rejected artifact changed the checkout")
		}
	}

	t.Run("applies and mirrors", func(t *testing.T) {
		src, dest := setup(t)
		if exit, out := apply(t, src, dest); exit != 0 {
			t.Fatalf("exit %d: %s", exit, out)
		}
		for path, want := range map[string]string{
			"schema/model.json":                       "new model",
			"docs/resources/a.md":                     "new a",
			"docs/resources/removed.md":               "<missing>",
			"internal/provider/zz_generated_specs.go": "new specs",
			"internal/provider/hooks.go":              "hooks",
			".github/workflows/x.yml":                 "workflow",
		} {
			if got := read(filepath.Join(dest, path)); got != want {
				t.Errorf("%s = %q, want %q", path, got, want)
			}
		}
	})

	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, src, dest string)
		out   string
	}{
		{"a file outside the allowed paths", func(t *testing.T, src, _ string) {
			write(t, filepath.Join(src, ".github/workflows/x.yml"), "evil")
		}, "unexpected name"},
		{"a sibling of an allowed file", func(t *testing.T, src, _ string) {
			write(t, filepath.Join(src, "internal/provider/hooks.go"), "evil")
		}, "outside the allowed paths"},
		{"a file next to an allowed directory", func(t *testing.T, src, _ string) {
			write(t, filepath.Join(src, "main.go"), "evil")
		}, "outside the allowed paths"},
		{"a symlink", func(t *testing.T, src, _ string) {
			if err := os.Symlink("/etc/passwd", filepath.Join(src, "docs/link.md")); err != nil {
				t.Fatal(err)
			}
		}, "is a symlink"},
		{"a symlinked directory", func(t *testing.T, src, _ string) {
			if err := os.Symlink("/etc", filepath.Join(src, "schema/upstream")); err != nil {
				t.Fatal(err)
			}
		}, "is a symlink"},
		{"an unusual file name", func(t *testing.T, src, _ string) {
			write(t, filepath.Join(src, "docs/a\nb.md"), "x")
		}, "unexpected name"},
		{"a missing allowed path", func(t *testing.T, src, _ string) {
			if err := os.RemoveAll(filepath.Join(src, "docs")); err != nil {
				t.Fatal(err)
			}
		}, "lacks docs"},
		{"a symlink in the checkout", func(t *testing.T, _, dest string) {
			if err := os.RemoveAll(filepath.Join(dest, "internal")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), filepath.Join(dest, "internal")); err != nil {
				t.Fatal(err)
			}
		}, "is a symlink in the checkout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, dest := setup(t)
			tc.setup(t, src, dest)
			exit, out := apply(t, src, dest)
			if exit == 0 || !strings.Contains(out, tc.out) {
				t.Errorf("exit %d, want a failure mentioning %q:\n%s", exit, tc.out, out)
			}
			unchanged(t, dest)
		})
	}

	t.Run("an allowed path that escapes", func(t *testing.T) {
		src, dest := setup(t)
		exit, out := runScript(t, nil, "", "apply-artifact.sh", src, dest, "../x")
		if exit == 0 || !strings.Contains(out, "invalid allowed path") {
			t.Errorf("exit %d: %s", exit, out)
		}
	})
}
