package gen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// OVERRIDES_PATH is the repository-relative path of the only hand-edited generator input.
const OVERRIDES_PATH = "schema/overrides.yaml"

// HOOKS_DIR is the package that implements named hooks.
const HOOKS_DIR = "internal/provider"

// Attribute modes.
const (
	MODE_REQUIRED          = "required"
	MODE_OPTIONAL          = "optional"
	MODE_COMPUTED          = "computed"
	MODE_OPTIONAL_COMPUTED = "optional_computed"
)

// Variable-name rules.
const (
	VAR_RULE_ENV    = "env_var"
	VAR_RULE_CONFIG = "config_var"
)

var (
	IDENT_RE = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	NAME_RE  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Overrides is schema/overrides.yaml.
type Overrides struct {
	// Scope lists the tables the provider manages. Every other table is reported as unscoped.
	Scope  []string                  `yaml:"scope"`
	Rules  RuleOverrides             `yaml:"rules"`
	Hooks  map[string]*HookDecl      `yaml:"hooks"`
	Tables map[string]*TableOverride `yaml:"tables"`
}

// RuleOverrides are field rules matched by table-name suffix.
type RuleOverrides struct {
	Sensitive []*FieldRule `yaml:"sensitive"`
	VarNames  []*FieldRule `yaml:"var_names"`
}

// FieldRule matches the named field of every in-scope table whose name ends with TableSuffix, or of the listed
// Tables.
type FieldRule struct {
	TableSuffix string   `yaml:"table_suffix"`
	Tables      []string `yaml:"tables"`
	Field       string `yaml:"field"`
	Rule        string `yaml:"rule"`
	Reason      string `yaml:"reason"`
}

// HookDecl declares a named Go hook and the API columns ("<table>.<field>") it uses.
type HookDecl struct {
	Uses   []string `yaml:"uses"`
	Reason string   `yaml:"reason"`
}

// TableOverride adjusts the rule result for one table.
type TableOverride struct {
	Reason string `yaml:"reason"`
	// Resource false keeps a writable table data-source only.
	Resource *bool `yaml:"resource"`
	// ResourceFields restricts the resource to these columns (the data source keeps every column).
	ResourceFields []string `yaml:"resource_fields"`
	// ImportBy imports the resource by this unique column instead of the numeric id.
	ImportBy string `yaml:"import_by"`
	// Hooks names Go hooks that replace a CRUD step.
	Hooks  map[string]string         `yaml:"hooks"`
	Fields map[string]*FieldOverride `yaml:"fields"`
}

// FieldOverride adjusts the rule result for one column.
type FieldOverride struct {
	Reason    string `yaml:"reason"`
	Mode      string `yaml:"mode"`
	ForceNew  *bool  `yaml:"force_new"`
	Sensitive *bool  `yaml:"sensitive"`
	Name      string `yaml:"name"`
	// ReferenceBy exposes a reference as this column of the referenced table instead of its id.
	ReferenceBy string `yaml:"reference_by"`
	// Resolver is a hook that resolves a ReferenceBy value to an id when the column is not unique on its own.
	Resolver string `yaml:"resolver"`
}

// matches reports whether the rule applies to a table.
func (r *FieldRule) matches(table string) bool {
	if r.TableSuffix != "" {
		return strings.HasSuffix(table, r.TableSuffix)
	}
	for _, t := range r.Tables {
		if t == table {
			return true
		}
	}
	return false
}

// HOOK_KINDS are the CRUD steps a table hook may replace.
var HOOK_KINDS = map[string]bool{"create": true, "delete": true}

// LoadOverrides reads and structurally validates the overrides file.
func LoadOverrides(repoDir string) (*Overrides, error) {
	data, err := os.ReadFile(filepath.Join(repoDir, OVERRIDES_PATH))
	if err != nil {
		return nil, err
	}
	return ParseOverrides(data)
}

// ParseOverrides decodes overrides strictly: unknown keys, duplicate keys and missing reasons are errors.
func ParseOverrides(data []byte) (*Overrides, error) {
	var o Overrides
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err != nil {
		return nil, fmt.Errorf("%s: %w", OVERRIDES_PATH, err)
	}
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	seen := map[string]bool{}
	for _, s := range o.Scope {
		if seen[s] {
			add("scope: %s listed twice", s)
		}
		seen[s] = true
	}
	for kind, rules := range map[string][]*FieldRule{"sensitive": o.Rules.Sensitive, "var_names": o.Rules.VarNames} {
		for i, r := range rules {
			if (r.TableSuffix == "") == (len(r.Tables) == 0) || r.Field == "" || strings.TrimSpace(r.Reason) == "" {
				add("rules.%s[%d]: one of table_suffix or tables, plus field and reason, are required", kind, i)
			}
			if kind == "var_names" && r.Rule != VAR_RULE_ENV && r.Rule != VAR_RULE_CONFIG {
				add("rules.var_names[%d]: rule must be %s or %s", i, VAR_RULE_ENV, VAR_RULE_CONFIG)
			}
			if kind == "sensitive" && r.Rule != "" {
				add("rules.sensitive[%d]: rule is not used here", i)
			}
		}
	}
	for name, h := range o.Hooks {
		if !IDENT_RE.MatchString(name) {
			add("hooks.%s: not a Go identifier", name)
		}
		if h == nil || strings.TrimSpace(h.Reason) == "" {
			add("hooks.%s: reason is required", name)
			continue
		}
		if len(h.Uses) == 0 {
			add("hooks.%s: uses must list the API columns the hook reads or writes", name)
		}
	}
	for table, t := range o.Tables {
		if t == nil {
			add("tables.%s: empty entry", table)
			continue
		}
		hasTableSetting := t.Resource != nil || len(t.ResourceFields) > 0 || t.ImportBy != "" || len(t.Hooks) > 0
		if hasTableSetting && strings.TrimSpace(t.Reason) == "" {
			add("tables.%s: reason is required", table)
		}
		if !hasTableSetting && t.Reason != "" {
			add("tables.%s: reason without a table-level setting", table)
		}
		if !hasTableSetting && len(t.Fields) == 0 {
			add("tables.%s: entry changes nothing", table)
		}
		for kind := range t.Hooks {
			if !HOOK_KINDS[kind] {
				add("tables.%s.hooks: unknown step %q", table, kind)
			}
		}
		for field, f := range t.Fields {
			if f == nil || strings.TrimSpace(f.Reason) == "" {
				add("tables.%s.fields.%s: reason is required", table, field)
				continue
			}
			switch f.Mode {
			case "", MODE_REQUIRED, MODE_OPTIONAL, MODE_COMPUTED, MODE_OPTIONAL_COMPUTED:
			default:
				add("tables.%s.fields.%s: unknown mode %q", table, field, f.Mode)
			}
			if f.Name != "" && !NAME_RE.MatchString(f.Name) {
				add("tables.%s.fields.%s: invalid attribute name %q", table, field, f.Name)
			}
			if f.Resolver != "" && f.ReferenceBy == "" {
				add("tables.%s.fields.%s: resolver needs reference_by", table, field)
			}
			if f.Mode == "" && f.ForceNew == nil && f.Sensitive == nil && f.Name == "" && f.ReferenceBy == "" {
				add("tables.%s.fields.%s: entry changes nothing", table, field)
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("%s:\n  %s", OVERRIDES_PATH, strings.Join(problems, "\n  "))
	}
	return &o, nil
}

// implementedHooks returns the names of top-level functions declared in the hooks package, ignoring generated files.
func implementedHooks(repoDir string) (map[string]bool, error) {
	dir := filepath.Join(repoDir, HOOKS_DIR)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "zz_generated") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				out[fn.Name.Name] = true
			}
		}
	}
	return out, nil
}
