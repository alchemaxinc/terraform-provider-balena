// Package sources reads, verifies and refreshes schema/sources.lock.json and the upstream files it pins.
//
// Verification is offline and deterministic. Fetch is the only code in the generator that uses the network.
package sources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// LOCK_PATH is the repository-relative path of the lock file.
const LOCK_PATH = "schema/sources.lock.json"

// UPSTREAM_DIR is the repository-relative directory that holds vendored upstream files.
const UPSTREAM_DIR = "schema/upstream"

// LOCK_FORMAT is the version of the lock file layout.
const LOCK_FORMAT = 1

// Source names.
const (
	SOURCE_OPEN_BALENA_API = "open-balena-api"
	SOURCE_BALENA_SDK      = "balena-sdk"
	SOURCE_BALENA_CLOUD    = "balena-cloud"
)

// File roles.
const (
	ROLE_SHAPE       = "shape"
	ROLE_CONSTRAINTS = "constraints"
	ROLE_PERMISSIONS = "permissions"
	ROLE_CONFIG_VARS = "config-vars"
)

// ROOT_UNVERSIONED is the name of the unversioned API root.
const ROOT_UNVERSIONED = "resin"

var (
	// ROOT_NAME_RE matches API root names.
	ROOT_NAME_RE = regexp.MustCompile(`^(resin|v[1-9][0-9]{0,2})$`)
	// TAG_RE matches an acceptable upstream tag.
	TAG_RE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
	// COMMIT_RE matches a full git commit SHA.
	COMMIT_RE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// SHA256_RE matches a hex sha256.
	SHA256_RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// UPSTREAM_PATH_RE matches an upstream file path.
	UPSTREAM_PATH_RE = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
)

// Lock is the content of schema/sources.lock.json.
type Lock struct {
	Format      int      `json:"format"`
	DefaultRoot string   `json:"default_root"`
	Roots       []Root   `json:"roots"`
	Sources     []Source `json:"sources"`
}

// Root describes one API root and the files its model is built from.
type Root struct {
	Name string `json:"name"`
	// Shape is the file ("<source>:<path>") that gives tables, fields, types and writability.
	Shape string `json:"shape"`
	// Constraints are SBVR files ("<source>:<path>") that give lengths, enums and uniqueness.
	Constraints []string `json:"constraints"`
	// Preview marks a root that may change without notice.
	Preview bool `json:"preview"`
}

// Source is one pinned upstream.
type Source struct {
	Name    string `json:"name"`
	Repo    string `json:"repo,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Commit  string `json:"commit,omitempty"`
	URL     string `json:"url,omitempty"`
	License string `json:"license"`
	Files   []File `json:"files"`
}

// File is one vendored file.
type File struct {
	Path   string `json:"path"`
	Local  string `json:"local"`
	Role   string `json:"role"`
	SHA256 string `json:"sha256"`
}

// Ref returns the "<source>:<path>" reference of a file.
func Ref(source, p string) string {
	return source + ":" + p
}

// ReadLock reads and validates the lock file under repoDir.
func ReadLock(repoDir string) (*Lock, error) {
	data, err := os.ReadFile(filepath.Join(repoDir, LOCK_PATH))
	if err != nil {
		return nil, err
	}
	var lock Lock
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&lock); err != nil {
		return nil, fmt.Errorf("%s: %w", LOCK_PATH, err)
	}
	if err := lock.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", LOCK_PATH, err)
	}
	return &lock, nil
}

// Validate checks the lock for internal consistency.
func (l *Lock) Validate() error {
	if l.Format != LOCK_FORMAT {
		return fmt.Errorf("unsupported format %d", l.Format)
	}
	refs := map[string]File{}
	locals := map[string]bool{}
	names := map[string]bool{}
	for _, s := range l.Sources {
		if names[s.Name] {
			return fmt.Errorf("duplicate source %s", s.Name)
		}
		names[s.Name] = true
		if s.Repo != "" {
			if !TAG_RE.MatchString(s.Tag) || !COMMIT_RE.MatchString(s.Commit) {
				return fmt.Errorf("source %s: invalid tag or commit", s.Name)
			}
		} else if s.URL == "" {
			return fmt.Errorf("source %s: needs a repo or a url", s.Name)
		}
		for _, f := range s.Files {
			if !UPSTREAM_PATH_RE.MatchString(f.Path) || !SHA256_RE.MatchString(f.SHA256) {
				return fmt.Errorf("source %s: invalid file entry %q", s.Name, f.Path)
			}
			if !strings.HasPrefix(f.Local, "schema/") || path.Clean(f.Local) != f.Local || strings.Contains(f.Local, "..") {
				return fmt.Errorf("source %s: invalid local path %q", s.Name, f.Local)
			}
			if locals[f.Local] {
				return fmt.Errorf("duplicate local path %s", f.Local)
			}
			locals[f.Local] = true
			refs[Ref(s.Name, f.Path)] = f
		}
	}
	rootNames := map[string]bool{}
	for _, r := range l.Roots {
		if !ROOT_NAME_RE.MatchString(r.Name) || rootNames[r.Name] {
			return fmt.Errorf("invalid or duplicate root %q", r.Name)
		}
		rootNames[r.Name] = true
		if f, ok := refs[r.Shape]; !ok || f.Role != ROLE_SHAPE {
			return fmt.Errorf("root %s: shape %s is not a locked shape file", r.Name, r.Shape)
		}
		for _, c := range r.Constraints {
			if f, ok := refs[c]; !ok || f.Role != ROLE_CONSTRAINTS {
				return fmt.Errorf("root %s: constraints %s is not a locked constraints file", r.Name, c)
			}
		}
	}
	if !rootNames[l.DefaultRoot] {
		return fmt.Errorf("default root %q is not a locked root", l.DefaultRoot)
	}
	if !rootNames[ROOT_UNVERSIONED] {
		return fmt.Errorf("the unversioned root %q is missing", ROOT_UNVERSIONED)
	}
	return nil
}

// Root returns the named root.
func (l *Lock) Root(name string) (Root, bool) {
	for _, r := range l.Roots {
		if r.Name == name {
			return r, true
		}
	}
	return Root{}, false
}

// Source returns the named source.
func (l *Lock) Source(name string) (Source, bool) {
	for _, s := range l.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return Source{}, false
}

// FileByRef returns the locked file with the given "<source>:<path>" reference.
func (l *Lock) FileByRef(ref string) (File, bool) {
	for _, s := range l.Sources {
		for _, f := range s.Files {
			if Ref(s.Name, f.Path) == ref {
				return f, true
			}
		}
	}
	return File{}, false
}

// FilesByRole returns every locked file with the given role, as "<source>:<path>" references.
func (l *Lock) FilesByRole(role string) []string {
	var out []string
	for _, s := range l.Sources {
		for _, f := range s.Files {
			if f.Role == role {
				out = append(out, Ref(s.Name, f.Path))
			}
		}
	}
	sort.Strings(out)
	return out
}

// Sha256Hex returns the hex sha256 of data.
func Sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Files reads every locked file, verifying its sha256, and fails when the vendored directory holds a file the lock
// does not pin. It returns the file contents keyed by "<source>:<path>".
func (l *Lock) Files(repoDir string) (map[string][]byte, error) {
	out := map[string][]byte{}
	locals := map[string]bool{}
	for _, s := range l.Sources {
		for _, f := range s.Files {
			data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(f.Local)))
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", f.Local, err)
			}
			if got := Sha256Hex(data); got != f.SHA256 {
				return nil, fmt.Errorf("%s: sha256 %s does not match the lock (%s); run `make fetch` to update the lock", f.Local, got, f.SHA256)
			}
			out[Ref(s.Name, f.Path)] = data
			locals[f.Local] = true
		}
	}
	err := filepath.WalkDir(filepath.Join(repoDir, UPSTREAM_DIR), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repoDir, p)
		if err != nil {
			return err
		}
		if !locals[filepath.ToSlash(rel)] {
			return fmt.Errorf("%s is not pinned by %s", filepath.ToSlash(rel), LOCK_PATH)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Marshal renders the lock deterministically.
func (l *Lock) Marshal() ([]byte, error) {
	sort.Slice(l.Sources, func(i, j int) bool { return l.Sources[i].Name < l.Sources[j].Name })
	for i := range l.Sources {
		files := l.Sources[i].Files
		sort.Slice(files, func(a, b int) bool { return files[a].Path < files[b].Path })
	}
	sort.Slice(l.Roots, func(i, j int) bool { return l.Roots[i].Name < l.Roots[j].Name })
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
