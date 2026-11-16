package sources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	FAKE_OBA_COMMIT = "1111111111111111111111111111111111111111"
	FAKE_SDK_COMMIT = "2222222222222222222222222222222222222222"
)

// fakeUpstream serves the GitHub API, raw.githubusercontent.com and /config/vars for Fetch.
func fakeUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	files := map[string]string{
		"/raw/" + OPEN_BALENA_API_REPO + "/" + FAKE_OBA_COMMIT + "/src/balena-model.ts":            "model",
		"/raw/" + OPEN_BALENA_API_REPO + "/" + FAKE_OBA_COMMIT + "/src/balena.sbvr":                "sbvr",
		"/raw/" + OPEN_BALENA_API_REPO + "/" + FAKE_OBA_COMMIT + "/src/lib/auth.ts":                "auth",
		"/raw/" + OPEN_BALENA_API_REPO + "/" + FAKE_OBA_COMMIT + "/src/translations/v7/v7.sbvr":    "v7 sbvr",
		"/raw/" + BALENA_SDK_REPO + "/" + FAKE_SDK_COMMIT + "/src/types/v7.ts":                     "v7 model",
		"/raw/" + BALENA_SDK_REPO + "/" + FAKE_SDK_COMMIT + "/src/types/v8.ts":                     "v8 model",
		"/api/repos/" + OPEN_BALENA_API_REPO + "/commits/v1.0.0":                                   FAKE_OBA_COMMIT,
		"/api/repos/" + BALENA_SDK_REPO + "/commits/v2.0.0":                                        FAKE_SDK_COMMIT,
		"/api/repos/" + OPEN_BALENA_API_REPO + "/contents/src/translations?ref=" + FAKE_OBA_COMMIT: `[{"name":"v6","type":"dir"},{"name":"v7","type":"dir"},{"name":"README.md","type":"file"}]`,
		"/api/repos/" + BALENA_SDK_REPO + "/contents/src/types?ref=" + FAKE_SDK_COMMIT:             `[{"name":"v7.ts","type":"file"},{"name":"v8.ts","type":"file"},{"name":"models.ts","type":"file"}]`,
		"/config/vars": `{"reservedNames":["RESIN"],"invalidRegex":"/\\W/"}`,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		body, ok := files[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}

func fetchInto(t *testing.T, dir string, srv *httptest.Server, defaultRoot string) *Lock {
	t.Helper()
	lock, err := Fetch(context.Background(), FetchOptions{
		RepoDir:          dir,
		OpenBalenaAPITag: "v1.0.0",
		BalenaSDKTag:     "v2.0.0",
		DefaultRoot:      defaultRoot,
		HTTPClient:       srv.Client(),
		GitHubAPIURL:     srv.URL + "/api",
		GitHubRawURL:     srv.URL + "/raw",
		ConfigVarsURL:    srv.URL + "/config/vars",
	})
	if err != nil {
		t.Fatal(err)
	}
	return lock
}

func TestFetchDiscoversRootsAndPinsFiles(t *testing.T) {
	srv := fakeUpstream(t)
	defer srv.Close()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := fetchInto(t, dir, srv, "")

	// v8 has a cloud shape but no translation: it becomes a root with only the unversioned constraints. v6 has a
	// translation but no cloud shape, so it is neither a root nor vendored. Without a previous lock the newest root
	// becomes the default.
	if lock.DefaultRoot != "v8" {
		t.Errorf("DefaultRoot = %s", lock.DefaultRoot)
	}
	var roots []string
	for _, r := range lock.Roots {
		roots = append(roots, r.Name+"="+strings.Join(r.Constraints, "+"))
	}
	want := "resin=open-balena-api:src/balena.sbvr v7=open-balena-api:src/balena.sbvr+open-balena-api:src/translations/v7/v7.sbvr v8=open-balena-api:src/balena.sbvr"
	if got := strings.Join(roots, " "); got != want {
		t.Errorf("roots = %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "schema/upstream/open-balena-api/src/translations/v6/v6.sbvr")); !os.IsNotExist(err) {
		t.Errorf("v6.sbvr should not be vendored")
	}
	data, err := os.ReadFile(filepath.Join(dir, "schema/upstream/open-balena-api/src/balena.sbvr"))
	if err != nil || string(data) != "sbvr" {
		t.Errorf("vendored balena.sbvr = %q, %v", data, err)
	}
	// The config vars snapshot is stored canonically.
	if cv, _ := os.ReadFile(filepath.Join(dir, "schema/upstream/balena-cloud/config-vars.json")); !strings.Contains(string(cv), "\n  \"invalidRegex\": \"/\\\\W/\",\n") {
		t.Errorf("config vars snapshot not canonical: %s", cv)
	}

	// The written lock reads back, verifies, and keeps the default root on the next fetch.
	reread, err := ReadLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reread.Files(dir); err != nil {
		t.Fatal(err)
	}
	if again := fetchInto(t, dir, srv, ""); again.DefaultRoot != "v8" {
		t.Errorf("default root not kept: %s", again.DefaultRoot)
	}
	if again := fetchInto(t, dir, srv, "v7"); again.DefaultRoot != "v7" {
		t.Errorf("default root override ignored: %s", again.DefaultRoot)
	}
}

func TestFilesFailsClosed(t *testing.T) {
	srv := fakeUpstream(t)
	defer srv.Close()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := fetchInto(t, dir, srv, "v7")

	tampered := filepath.Join(dir, "schema/upstream/balena-sdk/src/types/v7.ts")
	if err := os.WriteFile(tampered, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Files(dir); err == nil || !strings.Contains(err.Error(), "does not match the lock") {
		t.Errorf("tampered file: err = %v", err)
	}
	if err := os.WriteFile(tampered, []byte("v7 model"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schema/upstream/stray.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Files(dir); err == nil || !strings.Contains(err.Error(), "is not pinned") {
		t.Errorf("unpinned file: err = %v", err)
	}
}

func TestValidateRejectsBadLocks(t *testing.T) {
	good := func() *Lock {
		return &Lock{
			Format:      LOCK_FORMAT,
			DefaultRoot: "v7",
			Roots: []Root{
				{Name: "resin", Shape: "s:a.ts", Constraints: []string{"s:a.sbvr"}},
				{Name: "v7", Shape: "s:b.ts", Constraints: []string{"s:a.sbvr"}},
			},
			Sources: []Source{{Name: "s", Repo: "o/r", Tag: "v1", Commit: FAKE_OBA_COMMIT, Files: []File{
				{Path: "a.ts", Local: "schema/upstream/s/a.ts", Role: ROLE_SHAPE, SHA256: strings.Repeat("a", 64)},
				{Path: "b.ts", Local: "schema/upstream/s/b.ts", Role: ROLE_SHAPE, SHA256: strings.Repeat("b", 64)},
				{Path: "a.sbvr", Local: "schema/upstream/s/a.sbvr", Role: ROLE_CONSTRAINTS, SHA256: strings.Repeat("c", 64)},
			}}},
		}
	}
	if err := good().Validate(); err != nil {
		t.Fatalf("good lock: %v", err)
	}
	for name, mutate := range map[string]func(*Lock){
		"unknown default root":   func(l *Lock) { l.DefaultRoot = "v9" },
		"bad tag":                func(l *Lock) { l.Sources[0].Tag = "v1; rm -rf /" },
		"escaping local path":    func(l *Lock) { l.Sources[0].Files[0].Local = "schema/../../etc/passwd" },
		"local outside upstream": func(l *Lock) { l.Sources[0].Files[0].Local = "schema/a.ts" },
		"shape is constraints":   func(l *Lock) { l.Roots[1].Shape = "s:a.sbvr" },
		"no unversioned root":    func(l *Lock) { l.Roots = l.Roots[1:] },
	} {
		l := good()
		mutate(l)
		if err := l.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// TestFetchReplacesARetiredDefaultRoot covers upstream dropping the default root: the newest root becomes the default,
// and files of an earlier lock layout are removed.
func TestFetchReplacesARetiredDefaultRoot(t *testing.T) {
	srv := fakeUpstream(t)
	defer srv.Close()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := fetchInto(t, dir, srv, "v7")
	// Pretend the previous lock defaulted to a root upstream no longer publishes, and kept a file outside the
	// vendored directory.
	lock.DefaultRoot = "v6"
	lock.Roots = append(lock.Roots, Root{Name: "v6", Shape: lock.Roots[1].Shape, Constraints: lock.Roots[1].Constraints})
	lock.Sources[1].Files[0].Local = "schema/old-layout.sbvr"
	if err := os.WriteFile(filepath.Join(dir, "schema/old-layout.sbvr"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LOCK_PATH), data, 0o644); err != nil {
		t.Fatal(err)
	}
	again := fetchInto(t, dir, srv, "")
	if again.DefaultRoot != "v8" {
		t.Errorf("DefaultRoot = %s, want the newest root v8", again.DefaultRoot)
	}
	if _, err := os.Stat(filepath.Join(dir, "schema/old-layout.sbvr")); !os.IsNotExist(err) {
		t.Errorf("a file of the previous lock layout was left behind")
	}
}
