// Package gen builds the provider's intermediate representation (IR) of the balena API from pinned upstream models,
// classifies every table, derives the Terraform surface, and renders the generated files.
package gen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/alchemaxinc/terraform-provider-balena/internal/gen/sbvr"
	"github.com/alchemaxinc/terraform-provider-balena/internal/gen/sources"
	"github.com/alchemaxinc/terraform-provider-balena/internal/gen/tsmodel"
)

// IR_FORMAT is the version of the model.json layout.
const IR_FORMAT = 1

// Provenance values.
const (
	// PROVENANCE_OPEN marks a table or field that open-balena-api's model also has.
	PROVENANCE_OPEN = "open"
	// PROVENANCE_CLOUD marks a table or field that only balena-cloud's model (balena-sdk typings) has.
	PROVENANCE_CLOUD = "cloud-only"
)

// Field kinds.
const (
	KIND_SCALAR = "scalar"
	KIND_ENUM   = "enum"
	KIND_FK     = "fk"
)

// IR is the normalised model of every table the provider could manage.
type IR struct {
	Format      int          `json:"format"`
	DefaultRoot string       `json:"default_root"`
	Roots       []RootInfo   `json:"roots"`
	Sources     []SourceInfo `json:"sources"`
	Tables      []*Table     `json:"tables"`
	// byRoot keeps every root's own view of its tables, for coverage reporting.
	byRoot map[string]map[string]*Table
}

// RootInfo summarises an API root.
type RootInfo struct {
	Name        string   `json:"name"`
	Preview     bool     `json:"preview"`
	Shape       string   `json:"shape"`
	Constraints []string `json:"constraints"`
	// ModelGenerator is the abstract-sql-to-typescript version the shape file was generated with.
	ModelGenerator string `json:"model_generator"`
	Tables         int    `json:"tables"`
}

// SourceInfo summarises a pinned upstream.
type SourceInfo struct {
	Name   string `json:"name"`
	Tag    string `json:"tag,omitempty"`
	Commit string `json:"commit,omitempty"`
	URL    string `json:"url,omitempty"`
}

// Table is one API resource as seen through its chosen root.
type Table struct {
	// Name is the resource name used in URLs (term form when the resource has one).
	Name      string `json:"name"`
	Canonical string `json:"canonical"`
	Interface string `json:"interface"`
	// Root is the API root the provider uses for the table: the default root when it has the table, otherwise the
	// first fallback root that has it.
	Root string `json:"root"`
	// Roots lists every locked root that has the table.
	Roots      []string `json:"roots"`
	Provenance string   `json:"provenance"`
	Link       bool     `json:"link,omitempty"`
	// Writable is true when the root's model lets a user write at least one column.
	Writable bool     `json:"writable"`
	Fields   []*Field `json:"fields"`
	// NaturalKeys are the unique fields and, for link tables, the roles that together identify a row.
	NaturalKeys [][]string `json:"natural_keys,omitempty"`
	// OpenBalenaPermissions are the verbs open-balena-api grants a named user API key (informational).
	OpenBalenaPermissions []string `json:"open_balena_permissions,omitempty"`
	// Unenforced are SBVR necessities that are not column constraints (cross-table rules, numeric bounds).
	Unenforced []string `json:"unenforced,omitempty"`
	// Class is the generator's classification of the table.
	Class *Classification `json:"class,omitempty"`
}

// Field returns the named field.
func (t *Table) Field(name string) *Field {
	for _, f := range t.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// Field is one column of a table.
type Field struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Type is the SBVR type (Short Text, Integer, Boolean, Date Time, JSON, ...).
	Type string `json:"type"`
	// Target is the referenced table for references.
	Target    string   `json:"target,omitempty"`
	Nullable  bool     `json:"nullable"`
	Writable  bool     `json:"writable"`
	Computed  bool     `json:"computed,omitempty"`
	Enum      []string `json:"enum,omitempty"`
	MinLength int      `json:"min_length,omitempty"`
	MaxLength int      `json:"max_length,omitempty"`
	Unique    bool     `json:"unique,omitempty"`
	Notes     []string `json:"notes,omitempty"`
	// Provenance tells whether open-balena-api's model also has the column.
	Provenance string `json:"provenance"`
}

// Deprecated reports whether an SBVR note marks the field deprecated.
func (f *Field) Deprecated() bool {
	for _, n := range f.Notes {
		if strings.HasPrefix(strings.ToLower(n), "deprecated") {
			return true
		}
	}
	return false
}

// Table returns the named table.
func (ir *IR) Table(name string) *Table {
	for _, t := range ir.Tables {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// Inputs are the parsed upstream files.
type Inputs struct {
	Lock        *sources.Lock
	Models      map[string]*tsmodel.Model // by root
	SBVR        map[string]*sbvr.Model    // by "<source>:<path>"
	Permissions map[string][]string       // resin table -> verbs
	ConfigVars  *ConfigVars
}

// LoadInputs verifies the lock and parses every pinned file.
func LoadInputs(repoDir string) (*Inputs, error) {
	lock, err := sources.ReadLock(repoDir)
	if err != nil {
		return nil, err
	}
	files, err := lock.Files(repoDir)
	if err != nil {
		return nil, err
	}
	in := &Inputs{Lock: lock, Models: map[string]*tsmodel.Model{}, SBVR: map[string]*sbvr.Model{}}
	for _, r := range lock.Roots {
		m, err := tsmodel.Parse(string(files[r.Shape]))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.Shape, err)
		}
		in.Models[r.Name] = m
	}
	for _, ref := range lock.FilesByRole(sources.ROLE_CONSTRAINTS) {
		m, err := sbvr.Parse(string(files[ref]))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ref, err)
		}
		in.SBVR[ref] = m
	}
	perms := lock.FilesByRole(sources.ROLE_PERMISSIONS)
	if len(perms) != 1 {
		return nil, fmt.Errorf("expected one permissions file in the lock, found %d", len(perms))
	}
	if in.Permissions, err = readUserPermissions(string(files[perms[0]])); err != nil {
		return nil, err
	}
	cv := lock.FilesByRole(sources.ROLE_CONFIG_VARS)
	if len(cv) != 1 {
		return nil, fmt.Errorf("expected one config-vars file in the lock, found %d", len(cv))
	}
	if in.ConfigVars, err = parseConfigVars(files[cv[0]]); err != nil {
		return nil, err
	}
	return in, nil
}

// rootTables builds the tables of one root, cross-checking its TypeScript model against its SBVR files.
func (in *Inputs) rootTables(rootName string) (map[string]*Table, []string, error) {
	root, ok := in.Lock.Root(rootName)
	if !ok {
		return nil, nil, fmt.Errorf("root %q is not locked", rootName)
	}
	model := in.Models[rootName]
	var problems []string
	resinSBVR := in.SBVR[sources.Ref(sources.SOURCE_OPEN_BALENA_API, "src/balena.sbvr")]
	// The root's own SBVR is its last constraints file: vN.sbvr for a versioned root, balena.sbvr for resin.
	own := in.SBVR[root.Constraints[len(root.Constraints)-1]]
	resinModel := in.Models[sources.ROOT_UNVERSIONED]

	ifaceNames := map[string]bool{}
	for _, iface := range model.Canonical {
		ifaceNames[iface] = true
	}
	tables := map[string]*Table{}
	for iface := range ifaceNames {
		ti := model.Interfaces[iface]
		name, _ := model.ResourceName(iface)
		t := &Table{
			Name:       name,
			Canonical:  model.CanonicalName(iface),
			Interface:  iface,
			Root:       rootName,
			Provenance: PROVENANCE_CLOUD,
		}
		ownTable := sbvrTable(own, name, t.Canonical)
		resinTable := sbvrTable(resinSBVR, name, t.Canonical)
		if ownTable != nil || KNOWN_VIEWS[t.Canonical] || resinTable != nil {
			t.Provenance = PROVENANCE_OPEN
		}
		if ownTable != nil {
			t.Link = ownTable.Link
		}
		var resinIface *tsmodel.Interface
		if rn := resinModel.Synonyms[name]; rn != "" {
			resinIface = resinModel.Interfaces[rn]
		} else if rn := resinModel.Canonical[t.Canonical]; rn != "" {
			resinIface = resinModel.Interfaces[rn]
		}
		writes := map[string]tsmodel.Field{}
		for _, w := range ti.Write {
			writes[w.Name] = w
		}
		realFK := map[string]bool{}
		for _, f := range ti.Read {
			if f.Kind == tsmodel.KIND_FK && (hasWrite(writes, f.Name) || sbvrHas(ownTable, f.Name) || (resinIface != nil && resinWritable(resinIface, f.Name))) {
				realFK[f.Target] = true
			}
		}
		for _, f := range ti.Read {
			if f.Kind == tsmodel.KIND_REVERSE || f.Experimental {
				continue
			}
			key := name + "." + f.Name
			_, writable := writes[f.Name]
			inOwn := sbvrHas(ownTable, f.Name)
			inResin := resinIface != nil && hasRead(resinIface, f.Name)
			writableInResin := resinIface != nil && resinWritable(resinIface, f.Name)
			if f.Kind == tsmodel.KIND_FK && !writable && !inOwn && !KNOWN_COMPUTED[key] && !writableInResin {
				// A reference that the model neither writes nor declares is an alias of a real reference: a synonymous
				// form or the canonical expansion of a term form.
				if (ownTable != nil && contains(ownTable.Aliases, f.Name)) || realFK[f.Target] {
					continue
				}
			}
			fld := &Field{
				Name:       f.Name,
				Type:       f.Type,
				Nullable:   f.Nullable,
				Writable:   writable && !AUTO_FIELDS[f.Name] && !KNOWN_COMPUTED[key],
				Computed:   KNOWN_COMPUTED[key],
				Provenance: PROVENANCE_CLOUD,
			}
			if inOwn || inResin || AUTO_FIELDS[f.Name] || KNOWN_COMPUTED[key] {
				fld.Provenance = PROVENANCE_OPEN
			}
			switch f.Kind {
			case tsmodel.KIND_FK:
				fld.Kind = KIND_FK
				fld.Type = "Integer"
				target, _ := model.ResourceName(f.Target)
				fld.Target = target
			case tsmodel.KIND_ENUM:
				fld.Kind = KIND_ENUM
				fld.Enum = append([]string{}, f.Enum...)
			default:
				fld.Kind = KIND_SCALAR
			}
			// Constraints: the unversioned model's SBVR is what the database enforces; the root's own SBVR may add more.
			for _, st := range []*sbvr.Table{resinTable, ownTable} {
				if sf := sbvrField(st, name, f.Name); sf != nil {
					if sf.MinLength > fld.MinLength {
						fld.MinLength = sf.MinLength
					}
					if sf.MaxLength > 0 && (fld.MaxLength == 0 || sf.MaxLength < fld.MaxLength) {
						fld.MaxLength = sf.MaxLength
					}
					fld.Unique = fld.Unique || sf.Unique
					fld.Notes = mergeStrings(fld.Notes, sf.Notes)
				}
			}
			t.Fields = append(t.Fields, fld)
		}
		sort.Slice(t.Fields, func(i, j int) bool { return t.Fields[i].Name < t.Fields[j].Name })
		for _, f := range t.Fields {
			if f.Writable {
				t.Writable = true
			}
			if f.Unique && f.Name != "id" {
				t.NaturalKeys = append(t.NaturalKeys, []string{f.Name})
			}
		}
		if ownTable != nil && ownTable.Link {
			var roles []string
			for _, sf := range ownTable.Fields {
				if sf.Key {
					roles = append(roles, renamed(name, sf.Name))
				}
			}
			if len(roles) > 1 {
				t.NaturalKeys = append(t.NaturalKeys, roles)
			}
		}
		for _, st := range []*sbvr.Table{resinTable, ownTable} {
			if st != nil {
				t.Unenforced = mergeStrings(t.Unenforced, st.Unsupported)
			}
		}
		t.OpenBalenaPermissions = in.Permissions[name]
		if t.OpenBalenaPermissions == nil {
			t.OpenBalenaPermissions = in.Permissions[t.Canonical]
		}
		tables[name] = t
	}
	problems = append(problems, crossCheck(rootName, model, own, tables)...)
	return tables, problems, nil
}

func hasWrite(writes map[string]tsmodel.Field, name string) bool {
	_, ok := writes[name]
	return ok
}

func hasRead(i *tsmodel.Interface, name string) bool {
	_, ok := i.ReadField(name)
	return ok
}

func resinWritable(i *tsmodel.Interface, name string) bool {
	_, ok := i.WriteField(name)
	return ok
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func mergeStrings(a, b []string) []string {
	for _, s := range b {
		if !contains(a, s) {
			a = append(a, s)
		}
	}
	return a
}

// sbvrTable finds the SBVR table for a resource by its name or canonical name.
func sbvrTable(m *sbvr.Model, name, canonical string) *sbvr.Table {
	if m == nil {
		return nil
	}
	if t, ok := m.Tables[name]; ok {
		return t
	}
	for _, t := range m.Tables {
		if t.Canonical == canonical {
			return t
		}
	}
	return nil
}

// renamed maps an SBVR field name to the model's column name.
func renamed(table, field string) string {
	if to, ok := KNOWN_RENAMES[table+"."+field]; ok {
		return to
	}
	return field
}

// sbvrField finds the SBVR field behind a model column, following known renames.
func sbvrField(t *sbvr.Table, table, column string) *sbvr.Field {
	if t == nil {
		return nil
	}
	for _, f := range t.Fields {
		if renamed(table, f.Name) == column {
			return f
		}
	}
	return nil
}

func sbvrHas(t *sbvr.Table, column string) bool {
	if t == nil {
		return false
	}
	return sbvrField(t, t.Name, column) != nil
}

// BuildIR builds the IR. Each table comes from the first root in preference that has it; the default preference is the
// lock's default root followed by the unversioned root.
func BuildIR(in *Inputs, preference []string) (*IR, []string, error) {
	if len(preference) == 0 {
		preference = []string{in.Lock.DefaultRoot, sources.ROOT_UNVERSIONED}
	}
	ir := &IR{Format: IR_FORMAT, DefaultRoot: preference[0]}
	var problems []string
	byRoot := map[string]map[string]*Table{}
	for _, r := range in.Lock.Roots {
		tables, p, err := in.rootTables(r.Name)
		if err != nil {
			return nil, nil, err
		}
		problems = append(problems, p...)
		byRoot[r.Name] = tables
		ir.Roots = append(ir.Roots, RootInfo{
			Name:           r.Name,
			Preview:        r.Preview,
			Shape:          r.Shape,
			Constraints:    r.Constraints,
			ModelGenerator: in.Models[r.Name].GeneratorVersion,
			Tables:         len(tables),
		})
	}
	for _, s := range in.Lock.Sources {
		ir.Sources = append(ir.Sources, SourceInfo{Name: s.Name, Tag: s.Tag, Commit: s.Commit, URL: s.URL})
	}
	names := map[string]bool{}
	for _, tables := range byRoot {
		for n := range tables {
			names[n] = true
		}
	}
	for n := range names {
		var chosen *Table
		var roots []string
		for _, r := range in.Lock.Roots {
			if _, ok := byRoot[r.Name][n]; ok {
				roots = append(roots, r.Name)
			}
		}
		for _, r := range preference {
			if t, ok := byRoot[r][n]; ok {
				chosen = t
				break
			}
		}
		if chosen == nil {
			// Only in a root outside the preference (an older or not yet promoted root): not part of this IR.
			continue
		}
		chosen.Roots = roots
		ir.Tables = append(ir.Tables, chosen)
	}
	sort.Slice(ir.Tables, func(i, j int) bool { return ir.Tables[i].Name < ir.Tables[j].Name })
	ir.byRoot = byRoot
	sort.Strings(problems)
	return ir, problems, nil
}
