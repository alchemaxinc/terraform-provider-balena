package gen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// SURFACE_LOCK_PATH is the repository-relative path of the committed Terraform surface.
const SURFACE_LOCK_PATH = "schema/surface.lock.json"

// SURFACE_FORMAT is the version of the surface lock layout.
const SURFACE_FORMAT = 1

// Change levels, ordered by severity; they map to conventional commit types.
const (
	LEVEL_NONE     = "none"
	LEVEL_FIX      = "fix"
	LEVEL_FEAT     = "feat"
	LEVEL_BREAKING = "feat!"
)

var LEVEL_ORDER = map[string]int{LEVEL_NONE: 0, LEVEL_FIX: 1, LEVEL_FEAT: 2, LEVEL_BREAKING: 3}

// SurfaceLock is the Terraform-visible surface of the provider. Its attribute names are authoritative: a later
// generation keeps them for the same API field.
type SurfaceLock struct {
	Format      int                     `json:"format"`
	Resources   map[string]*SurfaceType `json:"resources"`
	DataSources map[string]*SurfaceType `json:"data_sources"`
}

// SurfaceType is one resource or data source.
type SurfaceType struct {
	Table      string                  `json:"table"`
	Root       string                  `json:"root"`
	Preview    bool                    `json:"preview,omitempty"`
	ImportBy   string                  `json:"import_by,omitempty"`
	Hooks      []string                `json:"hooks,omitempty"`
	Attributes map[string]*SurfaceAttr `json:"attributes"`
}

// SurfaceAttr is one attribute.
type SurfaceAttr struct {
	Field      string   `json:"field"`
	Via        string   `json:"via,omitempty"`
	Type       string   `json:"type"`
	Mode       string   `json:"mode"`
	ForceNew   bool     `json:"force_new,omitempty"`
	Sensitive  bool     `json:"sensitive,omitempty"`
	Validators []string `json:"validators,omitempty"`
}

// ReadSurfaceLock reads a surface lock; a missing file gives an empty lock.
func ReadSurfaceLock(path string) (*SurfaceLock, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &SurfaceLock{Format: SURFACE_FORMAT, Resources: map[string]*SurfaceType{}, DataSources: map[string]*SurfaceType{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseSurfaceLock(data)
}

// ParseSurfaceLock decodes a surface lock strictly.
func ParseSurfaceLock(data []byte) (*SurfaceLock, error) {
	var l SurfaceLock
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		return nil, fmt.Errorf("surface lock: %w", err)
	}
	if l.Format != SURFACE_FORMAT {
		return nil, fmt.Errorf("surface lock: unsupported format %d", l.Format)
	}
	if l.Resources == nil {
		l.Resources = map[string]*SurfaceType{}
	}
	if l.DataSources == nil {
		l.DataSources = map[string]*SurfaceType{}
	}
	return &l, nil
}

// readRepoSurfaceLock reads the committed surface lock of a repository.
func readRepoSurfaceLock(repoDir string) (*SurfaceLock, error) {
	return ReadSurfaceLock(filepath.Join(repoDir, SURFACE_LOCK_PATH))
}

// names returns the locked attribute names of a Terraform type, keyed by API field (and the referenced column for
// references exposed by a column).
func (l *SurfaceLock) names(typeName string) map[string]string {
	out := map[string]string{}
	if l == nil {
		return out
	}
	for _, st := range []*SurfaceType{l.Resources[typeName], l.DataSources[typeName]} {
		if st == nil {
			continue
		}
		for name, a := range st.Attributes {
			key := a.Field
			if a.Via != "" {
				key += "." + a.Via
			}
			out[key] = name
		}
	}
	return out
}

// BuildSurface renders the Terraform surface of the specs.
func BuildSurface(specs []*Spec) *SurfaceLock {
	l := &SurfaceLock{Format: SURFACE_FORMAT, Resources: map[string]*SurfaceType{}, DataSources: map[string]*SurfaceType{}}
	for _, s := range specs {
		hooks := []string{}
		for _, step := range sortedKeys(s.Hooks) {
			hooks = append(hooks, step+"="+s.Hooks[step])
		}
		if len(hooks) == 0 {
			hooks = nil
		}
		if s.Resource {
			st := &SurfaceType{Table: s.Table, Root: s.Root, Preview: s.Preview, ImportBy: s.ImportBy, Hooks: hooks, Attributes: map[string]*SurfaceAttr{}}
			for _, a := range s.Attributes {
				if a.ResourceMode != "" {
					st.Attributes[a.Name] = surfaceAttr(a, a.ResourceMode)
				}
			}
			l.Resources[s.TypeName] = st
		}
		if s.DataSource {
			st := &SurfaceType{Table: s.Table, Root: s.Root, Preview: s.Preview, Attributes: map[string]*SurfaceAttr{}}
			for _, a := range s.Attributes {
				sa := surfaceAttr(a, a.DataSourceMode)
				sa.ForceNew = false
				sa.Validators = nil
				st.Attributes[a.Name] = sa
			}
			l.DataSources[s.TypeName] = st
		}
	}
	return l
}

func surfaceAttr(a *Attr, mode string) *SurfaceAttr {
	sa := &SurfaceAttr{Field: a.Field, Type: a.Type, Mode: mode, ForceNew: a.ForceNew, Sensitive: a.Sensitive}
	if a.Encoding == ENC_REF_BY {
		sa.Via = a.RefField
	}
	if a.MinLength > 0 || a.MaxLength > 0 {
		v := "length:" + strconv.Itoa(a.MinLength) + ".."
		if a.MaxLength > 0 {
			v += strconv.Itoa(a.MaxLength)
		}
		sa.Validators = append(sa.Validators, v)
	}
	if len(a.OneOf) > 0 {
		vals := append([]string{}, a.OneOf...)
		sort.Strings(vals)
		sa.Validators = append(sa.Validators, "one_of:"+strings.Join(vals, ","))
	}
	if a.VarName != nil {
		sa.Validators = append(sa.Validators, "var_name:"+a.VarName.Describe())
	}
	if a.DateTime && a.Writable() {
		sa.Validators = append(sa.Validators, "date_time:rfc3339")
	}
	return sa
}

// Marshal renders the surface lock deterministically.
func (l *SurfaceLock) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Change is one difference between two surfaces.
type Change struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// SurfaceDiff compares two surfaces and returns every change and the overall level, which is the conventional commit
// type the change needs: feat! for anything that can break an existing configuration or state, feat for additions,
// fix for loosened validation.
func SurfaceDiff(old, new *SurfaceLock) ([]Change, string) {
	var changes []Change
	add := func(level, format string, args ...any) {
		changes = append(changes, Change{Level: level, Message: fmt.Sprintf(format, args...)})
	}
	for _, kind := range []struct {
		name     string
		old, new map[string]*SurfaceType
		resource bool
	}{{"resource", old.Resources, new.Resources, true}, {"data source", old.DataSources, new.DataSources, false}} {
		for _, tn := range unionKeys(kind.old, kind.new) {
			o, n := kind.old[tn], kind.new[tn]
			switch {
			case n == nil:
				add(LEVEL_BREAKING, "%s %s removed", kind.name, tn)
				continue
			case o == nil:
				add(LEVEL_FEAT, "%s %s added", kind.name, tn)
				continue
			}
			if o.Table != n.Table || o.Root != n.Root {
				add(LEVEL_FEAT, "%s %s moves from /%s/%s to /%s/%s", kind.name, tn, o.Root, o.Table, n.Root, n.Table)
			}
			if o.Preview != n.Preview {
				add(LEVEL_FIX, "%s %s preview %v -> %v", kind.name, tn, o.Preview, n.Preview)
			}
			if o.ImportBy != n.ImportBy {
				add(LEVEL_BREAKING, "%s %s imports by %q instead of %q", kind.name, tn, n.ImportBy, o.ImportBy)
			}
			if strings.Join(o.Hooks, ",") != strings.Join(n.Hooks, ",") {
				add(LEVEL_BREAKING, "%s %s hooks %v -> %v", kind.name, tn, o.Hooks, n.Hooks)
			}
			for _, an := range unionKeys(o.Attributes, n.Attributes) {
				diffAttr(add, kind.name+" "+tn+"."+an, o.Attributes[an], n.Attributes[an], kind.resource)
			}
		}
	}
	level := LEVEL_NONE
	for _, c := range changes {
		if LEVEL_ORDER[c.Level] > LEVEL_ORDER[level] {
			level = c.Level
		}
	}
	sort.SliceStable(changes, func(i, j int) bool {
		if LEVEL_ORDER[changes[i].Level] != LEVEL_ORDER[changes[j].Level] {
			return LEVEL_ORDER[changes[i].Level] > LEVEL_ORDER[changes[j].Level]
		}
		return changes[i].Message < changes[j].Message
	})
	return changes, level
}

func diffAttr(add func(string, string, ...any), what string, o, n *SurfaceAttr, resource bool) {
	switch {
	case n == nil:
		add(LEVEL_BREAKING, "%s removed", what)
		return
	case o == nil:
		if resource && n.Mode == MODE_REQUIRED {
			add(LEVEL_BREAKING, "%s added as required", what)
		} else {
			add(LEVEL_FEAT, "%s added", what)
		}
		return
	}
	if o.Field != n.Field || o.Via != n.Via {
		add(LEVEL_BREAKING, "%s maps to %s%s instead of %s%s", what, n.Field, viaSuffix(n.Via), o.Field, viaSuffix(o.Via))
	}
	if o.Type != n.Type {
		add(LEVEL_BREAKING, "%s type %s -> %s", what, o.Type, n.Type)
	}
	if o.Mode != n.Mode {
		level := LEVEL_BREAKING
		switch {
		case !resource && o.Mode == DS_COMPUTED:
			level = LEVEL_FEAT
		case resource && o.Mode == MODE_REQUIRED:
			level = LEVEL_FEAT
		case resource && o.Mode == MODE_COMPUTED && n.Mode != MODE_REQUIRED:
			level = LEVEL_FEAT
		case resource && o.Mode == MODE_OPTIONAL && n.Mode == MODE_OPTIONAL_COMPUTED:
			level = LEVEL_FEAT
		}
		add(level, "%s mode %s -> %s", what, o.Mode, n.Mode)
	}
	if o.ForceNew != n.ForceNew {
		level := LEVEL_FEAT
		if n.ForceNew {
			level = LEVEL_BREAKING
		}
		add(level, "%s force_new %v -> %v", what, o.ForceNew, n.ForceNew)
	}
	if o.Sensitive != n.Sensitive {
		level := LEVEL_FIX
		if !n.Sensitive {
			level = LEVEL_BREAKING
		}
		add(level, "%s sensitive %v -> %v", what, o.Sensitive, n.Sensitive)
	}
	diffValidators(add, what, o.Validators, n.Validators)
}

func viaSuffix(via string) string {
	if via == "" {
		return ""
	}
	return "." + via
}

func diffValidators(add func(string, string, ...any), what string, o, n []string) {
	byKind := func(list []string) map[string]string {
		m := map[string]string{}
		for _, v := range list {
			k, val, _ := strings.Cut(v, ":")
			m[k] = val
		}
		return m
	}
	om, nm := byKind(o), byKind(n)
	for _, k := range unionKeys(om, nm) {
		ov, oOK := om[k]
		nv, nOK := nm[k]
		switch {
		case ov == nv && oOK == nOK:
			continue
		case !nOK:
			add(LEVEL_FIX, "%s validator %s removed", what, k)
		case !oOK:
			add(LEVEL_BREAKING, "%s validator %s:%s added", what, k, nv)
		case k == "length" && lengthLoosened(ov, nv):
			add(LEVEL_FIX, "%s length %s -> %s", what, ov, nv)
		case k == "one_of" && supersetOf(strings.Split(nv, ","), strings.Split(ov, ",")):
			add(LEVEL_FIX, "%s one_of %s -> %s", what, ov, nv)
		default:
			add(LEVEL_BREAKING, "%s validator %s %s -> %s", what, k, ov, nv)
		}
	}
}

// lengthLoosened reports whether the bounds "min..max" (empty max = none) got wider.
func lengthLoosened(o, n string) bool {
	parse := func(s string) (int, int) {
		lo, hi, _ := strings.Cut(s, "..")
		a, _ := strconv.Atoi(lo)
		b, _ := strconv.Atoi(hi)
		return a, b
	}
	omin, omax := parse(o)
	nmin, nmax := parse(n)
	return nmin <= omin && (nmax == 0 || (omax != 0 && nmax >= omax))
}

func supersetOf(super, sub []string) bool {
	set := map[string]bool{}
	for _, s := range super {
		set[s] = true
	}
	for _, s := range sub {
		if !set[s] {
			return false
		}
	}
	return true
}

func unionKeys[V any](a, b map[string]V) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
