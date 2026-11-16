package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MAX_DOWNLOAD_BYTES bounds every download.
const MAX_DOWNLOAD_BYTES = 16 << 20

// FETCH_TIMEOUT bounds every HTTP request made by Fetch.
const FETCH_TIMEOUT = 60 * time.Second

var (
	// SDK_ROOT_FILE_RE matches the per-root model files of balena-sdk (src/types/v7.ts).
	SDK_ROOT_FILE_RE = regexp.MustCompile(`^v([1-9][0-9]{0,2})\.ts$`)
	// TRANSLATION_DIR_RE matches the per-root translation directories of open-balena-api (src/translations/v7).
	TRANSLATION_DIR_RE = regexp.MustCompile(`^v([1-9][0-9]{0,2})$`)
)

// Upstream locations.
const (
	OPEN_BALENA_API_REPO = "balena-io/open-balena-api"
	BALENA_SDK_REPO      = "balena-io/balena-sdk"
	CONFIG_VARS_URL      = "https://api.balena-cloud.com/config/vars"
	GITHUB_API           = "https://api.github.com"
	GITHUB_RAW           = "https://raw.githubusercontent.com"
)

// Fixed upstream files, by source.
var (
	OPEN_BALENA_API_FILES = []File{
		{Path: "src/balena-model.ts", Role: ROLE_SHAPE},
		{Path: "src/balena.sbvr", Role: ROLE_CONSTRAINTS},
		{Path: "src/lib/auth.ts", Role: ROLE_PERMISSIONS},
	}
)

// FetchOptions selects what Fetch pins.
type FetchOptions struct {
	// RepoDir is the repository root.
	RepoDir string
	// OpenBalenaAPITag and BalenaSDKTag pin the git sources. Empty keeps the locked tag.
	OpenBalenaAPITag string
	BalenaSDKTag     string
	// Latest resolves the newest tag of each git source instead.
	Latest bool
	// DefaultRoot overrides the default root. Empty keeps the locked default, or picks the newest versioned root.
	DefaultRoot string
	// GitHubToken authenticates GitHub API calls when set.
	GitHubToken string
	// HTTPClient is used for every request.
	HTTPClient *http.Client
	// Log receives progress lines.
	Log io.Writer
	// GitHubAPIURL, GitHubRawURL and ConfigVarsURL override the upstream endpoints (tests).
	GitHubAPIURL  string
	GitHubRawURL  string
	ConfigVarsURL string
}

type fetcher struct {
	opts FetchOptions
	ctx  context.Context
}

// Fetch downloads the pinned upstream files, rewrites the vendored copies and the lock, and returns the new lock.
func Fetch(ctx context.Context, opts FetchOptions) (*Lock, error) {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: FETCH_TIMEOUT}
	}
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	if opts.GitHubAPIURL == "" {
		opts.GitHubAPIURL = GITHUB_API
	}
	if opts.GitHubRawURL == "" {
		opts.GitHubRawURL = GITHUB_RAW
	}
	if opts.ConfigVarsURL == "" {
		opts.ConfigVarsURL = CONFIG_VARS_URL
	}
	f := &fetcher{opts: opts, ctx: ctx}

	// The previous lock is read without validation: it may predate the current layout, and only its tags, its default
	// root and its local paths (for cleanup) are used.
	old, err := readPreviousLock(opts.RepoDir)
	if err != nil {
		return nil, err
	}
	obaTag, sdkTag := opts.OpenBalenaAPITag, opts.BalenaSDKTag
	if old != nil {
		if s, ok := old.Source(SOURCE_OPEN_BALENA_API); ok && obaTag == "" {
			obaTag = s.Tag
		}
		if s, ok := old.Source(SOURCE_BALENA_SDK); ok && sdkTag == "" {
			sdkTag = s.Tag
		}
	}
	if opts.Latest {
		if obaTag, err = f.latestTag(OPEN_BALENA_API_REPO); err != nil {
			return nil, err
		}
		if sdkTag, err = f.latestTag(BALENA_SDK_REPO); err != nil {
			return nil, err
		}
	}
	if obaTag == "" || sdkTag == "" {
		return nil, fmt.Errorf("no tags to fetch: pass -open-balena-api and -balena-sdk, or -latest")
	}

	lock := &Lock{Format: LOCK_FORMAT}
	files := map[string][]byte{}

	sdk, err := f.gitSource(SOURCE_BALENA_SDK, BALENA_SDK_REPO, sdkTag, "Apache-2.0")
	if err != nil {
		return nil, err
	}
	types, err := f.listDir(BALENA_SDK_REPO, sdk.Commit, "src/types")
	if err != nil {
		return nil, err
	}
	var sdkFiles []File
	sdkRoots := map[string]bool{}
	for _, entry := range types {
		if m := SDK_ROOT_FILE_RE.FindStringSubmatch(entry.Name); m != nil && entry.Type == "file" {
			sdkFiles = append(sdkFiles, File{Path: "src/types/" + entry.Name, Role: ROLE_SHAPE})
			sdkRoots["v"+m[1]] = true
		}
	}
	if len(sdkFiles) == 0 {
		return nil, fmt.Errorf("balena-sdk %s has no src/types/v<N>.ts model", sdkTag)
	}
	if err := f.download(&sdk, sdkFiles, files); err != nil {
		return nil, err
	}
	lock.Sources = append(lock.Sources, sdk)

	oba, err := f.gitSource(SOURCE_OPEN_BALENA_API, OPEN_BALENA_API_REPO, obaTag, "AGPL-3.0")
	if err != nil {
		return nil, err
	}
	obaFiles := append([]File{}, OPEN_BALENA_API_FILES...)
	translations, err := f.listDir(OPEN_BALENA_API_REPO, oba.Commit, "src/translations")
	if err != nil {
		return nil, err
	}
	versioned := map[string]bool{}
	for _, entry := range translations {
		// Only roots that balena-sdk types for balena-cloud become roots, so only their translations are vendored.
		if TRANSLATION_DIR_RE.MatchString(entry.Name) && entry.Type == "dir" && sdkRoots[entry.Name] {
			obaFiles = append(obaFiles, File{Path: "src/translations/" + entry.Name + "/" + entry.Name + ".sbvr", Role: ROLE_CONSTRAINTS})
			versioned[entry.Name] = true
		}
	}
	if err := f.download(&oba, obaFiles, files); err != nil {
		return nil, err
	}
	lock.Sources = append(lock.Sources, oba)

	cloud := Source{Name: SOURCE_BALENA_CLOUD, URL: CONFIG_VARS_URL, License: "public API response"}
	body, err := f.get(opts.ConfigVarsURL, "")
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalJSON(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opts.ConfigVarsURL, err)
	}
	cv := File{Path: "config/vars", Role: ROLE_CONFIG_VARS, Local: UPSTREAM_DIR + "/" + SOURCE_BALENA_CLOUD + "/config-vars.json", SHA256: Sha256Hex(canonical)}
	cloud.Files = []File{cv}
	files[cv.Local] = canonical
	lock.Sources = append(lock.Sources, cloud)

	// Roots: the unversioned model, plus every versioned root that has both a cloud shape (balena-sdk) and an
	// open-balena-api translation.
	lock.Roots = append(lock.Roots, Root{
		Name:        ROOT_UNVERSIONED,
		Shape:       Ref(SOURCE_OPEN_BALENA_API, "src/balena-model.ts"),
		Constraints: []string{Ref(SOURCE_OPEN_BALENA_API, "src/balena.sbvr")},
		Preview:     true,
	})
	newest := 0
	for _, sf := range sdkFiles {
		m := SDK_ROOT_FILE_RE.FindStringSubmatch(path.Base(sf.Path))
		name := "v" + m[1]
		constraints := []string{Ref(SOURCE_OPEN_BALENA_API, "src/balena.sbvr")}
		if versioned[name] {
			constraints = append(constraints, Ref(SOURCE_OPEN_BALENA_API, "src/translations/"+name+"/"+name+".sbvr"))
		}
		lock.Roots = append(lock.Roots, Root{Name: name, Shape: Ref(SOURCE_BALENA_SDK, sf.Path), Constraints: constraints})
		if n, _ := strconv.Atoi(m[1]); n > newest {
			newest = n
		}
	}
	switch {
	case opts.DefaultRoot != "":
		lock.DefaultRoot = opts.DefaultRoot
	case old != nil && lock.hasRoot(old.DefaultRoot):
		lock.DefaultRoot = old.DefaultRoot
	case old != nil:
		// The default root is no longer published: the newest root replaces it, and the contract gate decides whether
		// that root is usable.
		lock.DefaultRoot = "v" + strconv.Itoa(newest)
		_, _ = fmt.Fprintf(opts.Log, "default root %s is no longer published; defaulting to %s\n", old.DefaultRoot, lock.DefaultRoot)
	default:
		lock.DefaultRoot = "v" + strconv.Itoa(newest)
	}
	if err := lock.Validate(); err != nil {
		return nil, err
	}

	// Replace the vendored directory atomically enough for a developer tool: remove stale files, then write.
	if err := os.RemoveAll(filepath.Join(opts.RepoDir, UPSTREAM_DIR)); err != nil {
		return nil, err
	}
	if old != nil {
		// Earlier lock layouts kept files outside the vendored directory.
		for _, s := range old.Sources {
			for _, f := range s.Files {
				if _, kept := files[f.Local]; !kept && strings.HasPrefix(f.Local, "schema/") && path.Clean(f.Local) == f.Local && !strings.Contains(f.Local, "..") {
					if err := os.Remove(filepath.Join(opts.RepoDir, filepath.FromSlash(f.Local))); err != nil && !os.IsNotExist(err) {
						return nil, err
					}
				}
			}
		}
	}
	locals := make([]string, 0, len(files))
	for local := range files {
		locals = append(locals, local)
	}
	sort.Strings(locals)
	for _, local := range locals {
		p := filepath.Join(opts.RepoDir, filepath.FromSlash(local))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, files[local], 0o644); err != nil {
			return nil, err
		}
	}
	data, err := lock.Marshal()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(opts.RepoDir, LOCK_PATH), data, 0o644); err != nil {
		return nil, err
	}
	return lock, nil
}

// readPreviousLock reads the current lock file, if any, without validating it.
func readPreviousLock(repoDir string) (*Lock, error) {
	data, err := os.ReadFile(filepath.Join(repoDir, LOCK_PATH))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lock Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("%s: %w", LOCK_PATH, err)
	}
	return &lock, nil
}

func canonicalJSON(body []byte) ([]byte, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func (f *fetcher) gitSource(name, repo, tag, license string) (Source, error) {
	if !TAG_RE.MatchString(tag) {
		return Source{}, fmt.Errorf("refusing upstream tag %q", tag)
	}
	sha, err := f.get(f.opts.GitHubAPIURL+"/repos/"+repo+"/commits/"+tag, "application/vnd.github.sha")
	if err != nil {
		return Source{}, fmt.Errorf("resolving %s@%s: %w", repo, tag, err)
	}
	commit := strings.TrimSpace(string(sha))
	if !COMMIT_RE.MatchString(commit) {
		return Source{}, fmt.Errorf("resolving %s@%s: unexpected commit %q", repo, tag, commit)
	}
	_, _ = fmt.Fprintf(f.opts.Log, "%s %s -> %s\n", repo, tag, commit)
	return Source{Name: name, Repo: repo, Tag: tag, Commit: commit, License: license}, nil
}

func (f *fetcher) download(src *Source, wanted []File, files map[string][]byte) error {
	for _, w := range wanted {
		if !UPSTREAM_PATH_RE.MatchString(w.Path) {
			return fmt.Errorf("refusing upstream path %q", w.Path)
		}
		body, err := f.get(f.opts.GitHubRawURL+"/"+src.Repo+"/"+src.Commit+"/"+w.Path, "")
		if err != nil {
			return fmt.Errorf("downloading %s:%s: %w", src.Name, w.Path, err)
		}
		if w.Local == "" {
			w.Local = UPSTREAM_DIR + "/" + src.Name + "/" + w.Path
		}
		w.SHA256 = Sha256Hex(body)
		files[w.Local] = body
		src.Files = append(src.Files, w)
		_, _ = fmt.Fprintf(f.opts.Log, "  %s (%d bytes)\n", w.Local, len(body))
	}
	return nil
}

type dirEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func (f *fetcher) listDir(repo, commit, dir string) ([]dirEntry, error) {
	body, err := f.get(f.opts.GitHubAPIURL+"/repos/"+repo+"/contents/"+dir+"?ref="+commit, "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("listing %s/%s: %w", repo, dir, err)
	}
	var entries []dirEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("listing %s/%s: %w", repo, dir, err)
	}
	return entries, nil
}

func (f *fetcher) latestTag(repo string) (string, error) {
	body, err := f.get(f.opts.GitHubAPIURL+"/repos/"+repo+"/tags?per_page=1", "application/vnd.github+json")
	if err != nil {
		return "", err
	}
	var tags []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &tags); err != nil || len(tags) == 0 {
		return "", fmt.Errorf("listing tags of %s: %v", repo, err)
	}
	if !TAG_RE.MatchString(tags[0].Name) {
		return "", fmt.Errorf("refusing upstream tag %q of %s", tags[0].Name, repo)
	}
	return tags[0].Name, nil
}

func (f *fetcher) get(url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "terraform-provider-balena-schema-fetch")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if f.opts.GitHubToken != "" && strings.HasPrefix(url, f.opts.GitHubAPIURL) {
		req.Header.Set("Authorization", "Bearer "+f.opts.GitHubToken)
	}
	resp, err := f.opts.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MAX_DOWNLOAD_BYTES+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MAX_DOWNLOAD_BYTES {
		return nil, fmt.Errorf("%s: response larger than %d bytes", url, MAX_DOWNLOAD_BYTES)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return body, nil
}
