package gen

import (
	"fmt"
	"sort"
	"strings"
)

// Attribute value types.
const (
	TYPE_STRING  = "string"
	TYPE_INT64   = "int64"
	TYPE_BOOL    = "bool"
	TYPE_FLOAT64 = "float64"
)

// Attribute encodings: how an attribute value maps to the API field.
const (
	// ENC_PLAIN is a scalar column.
	ENC_PLAIN = "plain"
	// ENC_JSON is a JSON column rendered as a JSON string.
	ENC_JSON = "json"
	// ENC_REF is a reference rendered as the referenced id: {"__id": N} on read, N on write.
	ENC_REF = "ref"
	// ENC_REF_BY is a reference rendered as a column of the referenced row, resolved to an id on write.
	ENC_REF_BY = "ref_by"
)

// Data source attribute modes.
const (
	DS_FILTER   = "filter"
	DS_COMPUTED = "computed"
)

// TERRAFORM_RESERVED are root attribute names Terraform reserves on resources and data sources.
var TERRAFORM_RESERVED = map[string]bool{
	"connection": true, "count": true, "depends_on": true, "for_each": true, "lifecycle": true, "provider": true,
	"provisioner": true,
}

// SBVR_TYPES maps SBVR types to attribute types. A type missing here is unsupported.
var SBVR_TYPES = map[string]string{
	"Short Text":  TYPE_STRING,
	"Text":        TYPE_STRING,
	"Date Time":   TYPE_STRING,
	"Date":        TYPE_STRING,
	"JSON":        TYPE_STRING,
	"Integer":     TYPE_INT64,
	"Big Integer": TYPE_INT64,
	"Serial":      TYPE_INT64,
	"Big Serial":  TYPE_INT64,
	"Boolean":     TYPE_BOOL,
	"Real":        TYPE_FLOAT64,
}

// UNSUPPORTED_TYPES explains why some SBVR types get no attribute.
var UNSUPPORTED_TYPES = map[string]string{
	"WebResource": "file uploads (WebResource) are not supported",
	"Hashed":      "hashed values are write-only and cannot be read back",
}

// Spec is the Terraform surface of one table.
type Spec struct {
	TypeName              string            `json:"type_name"`
	Table                 string            `json:"table"`
	Root                  string            `json:"root"`
	Preview               bool              `json:"preview,omitempty"`
	Resource              bool              `json:"resource"`
	DataSource            bool              `json:"data_source"`
	ResourceDescription   string            `json:"-"`
	DataSourceDescription string            `json:"-"`
	ImportBy              string            `json:"import_by,omitempty"`
	Hooks                 map[string]string `json:"hooks,omitempty"`
	Attributes            []*Attr           `json:"attributes"`
	Skipped               []Skipped         `json:"skipped,omitempty"`
}

// Attr is one attribute.
type Attr struct {
	Name     string `json:"name"`
	Field    string `json:"field"`
	Type     string `json:"type"`
	Encoding string `json:"encoding"`
	RefTable string `json:"ref_table,omitempty"`
	RefField string `json:"ref_field,omitempty"`
	Resolver string `json:"resolver,omitempty"`
	// ResourceMode is empty when the attribute is not part of the resource.
	ResourceMode   string       `json:"resource_mode,omitempty"`
	DataSourceMode string       `json:"data_source_mode"`
	ForceNew       bool         `json:"force_new,omitempty"`
	Sensitive      bool         `json:"sensitive,omitempty"`
	MinLength      int          `json:"min_length,omitempty"`
	MaxLength      int          `json:"max_length,omitempty"`
	OneOf          []string     `json:"one_of,omitempty"`
	VarName        *VarNameRule `json:"var_name,omitempty"`
	// Stable marks a computed value that never changes once set (ids, creation time, server-generated identifiers).
	Stable      bool   `json:"stable,omitempty"`
	Description string `json:"-"`
	// sbvrType keeps the source type for descriptions.
	sbvrType string
	field    *Field
}

// Skipped is a column that gets no attribute.
type Skipped struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Writable reports whether users set the attribute on the resource.
func (a *Attr) Writable() bool {
	return a.ResourceMode == MODE_REQUIRED || a.ResourceMode == MODE_OPTIONAL || a.ResourceMode == MODE_OPTIONAL_COMPUTED
}

// key identifies an attribute across generations for sticky names.
func (a *Attr) key() string {
	if a.Encoding == ENC_REF_BY {
		return a.Field + "." + a.RefField
	}
	return a.Field
}

// BuildSpecs derives the Terraform surface of every generated or hand-written table.
func BuildSpecs(ir *IR, o *Overrides, cv *ConfigVars, old *SurfaceLock, hooks map[string]bool) ([]*Spec, error) {
	var specs []*Spec
	var problems []string
	usedRules := map[*FieldRule]bool{}
	usedHooks := map[string]bool{}
	for _, t := range ir.Tables {
		if t.Class.Status != STATUS_GENERATED && t.Class.Status != STATUS_HANDWRITTEN {
			continue
		}
		s, p := buildSpec(ir, t, o, cv, old, hooks, usedRules, usedHooks)
		problems = append(problems, p...)
		if s != nil {
			specs = append(specs, s)
		}
	}
	for kind, rules := range map[string][]*FieldRule{"sensitive": o.Rules.Sensitive, "var_names": o.Rules.VarNames} {
		for i, r := range rules {
			if !usedRules[r] {
				problems = append(problems, fmt.Sprintf("rules.%s[%d] matches no in-scope column", kind, i))
			}
			for _, table := range r.Tables {
				found := false
				for _, s := range specs {
					for _, a := range s.Attributes {
						found = found || (s.Table == table && a.Field == r.Field)
					}
				}
				if !found {
					problems = append(problems, fmt.Sprintf("rules.%s[%d]: %s.%s is not an in-scope column", kind, i, table, r.Field))
				}
			}
		}
	}
	for name, h := range o.Hooks {
		if !usedHooks[name] {
			problems = append(problems, fmt.Sprintf("hooks.%s is declared but no table uses it", name))
		}
		if !hooks[name] {
			problems = append(problems, fmt.Sprintf("hooks.%s has no implementation: no func %s in %s", name, name, HOOKS_DIR))
		}
		for _, use := range h.Uses {
			table, field, ok := strings.Cut(use, ".")
			tb := ir.Table(table)
			if !ok || tb == nil || tb.Field(field) == nil {
				problems = append(problems, fmt.Sprintf("hooks.%s uses %s, which is not an IR column", name, use))
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].TypeName < specs[j].TypeName })
	return specs, nil
}

func buildSpec(ir *IR, t *Table, o *Overrides, cv *ConfigVars, old *SurfaceLock, hooks map[string]bool,
	usedRules map[*FieldRule]bool, usedHooks map[string]bool) (*Spec, []string) {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf("%s: ", t.Name)+fmt.Sprintf(format, args...))
	}
	to := o.Tables[t.Name]
	if to == nil {
		to = &TableOverride{}
	}
	s := &Spec{
		TypeName:   t.Class.TypeName,
		Table:      t.Name,
		Root:       t.Root,
		Preview:    t.Class.Preview,
		Resource:   t.Class.Resource,
		DataSource: t.Class.DataSource,
	}
	// Columns.
	for _, f := range t.Fields {
		if f.Kind == KIND_FK {
			target := ir.Table(f.Target)
			if target == nil {
				add("%s references unknown table %s", f.Name, f.Target)
				continue
			}
			if !hasDataColumns(target) {
				s.Skipped = append(s.Skipped, Skipped{f.Name, "references " + f.Target + ", which has no columns besides its id"})
				continue
			}
		}
		if f.Name == "modified_at" {
			s.Skipped = append(s.Skipped, Skipped{f.Name, "changes on every write, and balena-cloud does not let users read it on every table"})
			continue
		}
		if isPaymentField(f.Name) {
			s.Skipped = append(s.Skipped, Skipped{f.Name, "payment or billing column"})
			continue
		}
		if reason, ok := UNSUPPORTED_TYPES[f.Type]; ok {
			s.Skipped = append(s.Skipped, Skipped{f.Name, reason})
			continue
		}
		typ, ok := SBVR_TYPES[f.Type]
		if !ok {
			add("%s has unsupported type %q", f.Name, f.Type)
			continue
		}
		a := &Attr{Field: f.Name, Type: typ, Encoding: ENC_PLAIN, sbvrType: f.Type, field: f}
		switch {
		case f.Kind == KIND_FK:
			a.Encoding = ENC_REF
			a.RefTable = f.Target
		case f.Type == "JSON":
			a.Encoding = ENC_JSON
		}
		if fo := to.Fields[f.Name]; fo != nil && fo.ReferenceBy != "" {
			target := ir.Table(f.Target)
			switch {
			case f.Kind != KIND_FK:
				add("fields.%s.reference_by on a column that is not a reference", f.Name)
			case target.Field(fo.ReferenceBy) == nil:
				add("fields.%s.reference_by: %s has no column %s", f.Name, f.Target, fo.ReferenceBy)
			case fo.Resolver == "" && !target.Field(fo.ReferenceBy).Unique:
				add("fields.%s.reference_by: %s.%s is not unique, so it needs a resolver hook", f.Name, f.Target, fo.ReferenceBy)
			default:
				a.Encoding = ENC_REF_BY
				a.RefField = fo.ReferenceBy
				a.Type = SBVR_TYPES[target.Field(fo.ReferenceBy).Type]
				a.Resolver = fo.Resolver
				if fo.Resolver != "" {
					if o.Hooks[fo.Resolver] == nil {
						add("fields.%s.resolver %s is not declared under hooks", f.Name, fo.Resolver)
					}
					usedHooks[fo.Resolver] = true
				}
			}
		}
		s.Attributes = append(s.Attributes, a)
	}
	for field := range to.Fields {
		found := false
		for _, a := range s.Attributes {
			found = found || a.Field == field
		}
		if !found {
			add("fields.%s: no such exposed column", field)
		}
	}
	if err := nameAttributes(s, to, old); err != nil {
		add("%v", err)
	}

	// Resource subset.
	inResource := map[string]bool{}
	if len(to.ResourceFields) > 0 {
		if !s.Resource {
			add("resource_fields on a table without a resource")
		}
		for _, f := range to.ResourceFields {
			if t.Field(f) == nil {
				add("resource_fields: no column %s", f)
			}
			inResource[f] = true
		}
	}
	naturalKey := map[string]bool{}
	if t.Link {
		for _, k := range t.NaturalKeys {
			if len(k) > 1 {
				for _, f := range k {
					naturalKey[f] = true
				}
			}
		}
	}
	for _, a := range s.Attributes {
		f := a.field
		fo := to.Fields[f.Name]
		a.Stable = f.Name == "id" || f.Name == "created_at" || (f.Unique && !f.Nullable)
		a.DataSourceMode = DS_COMPUTED
		if (a.Encoding == ENC_PLAIN || a.Encoding == ENC_REF) && a.sbvrType != "Date Time" && a.sbvrType != "Date" {
			a.DataSourceMode = DS_FILTER
		}
		if !s.Resource || (len(inResource) > 0 && !inResource[f.Name] && f.Name != "id") {
			if fo != nil && (fo.Mode != "" || fo.ForceNew != nil) {
				add("fields.%s sets resource properties on a column outside the resource", f.Name)
			}
		} else {
			a.ResourceMode = ruleMode(f, a)
			if fo != nil && fo.Mode != "" {
				if fo.Mode == a.ResourceMode {
					add("fields.%s.mode %s is a no-op", f.Name, fo.Mode)
				}
				a.ResourceMode = fo.Mode
			}
			a.ForceNew = a.Writable() && ((a.ResourceMode == MODE_REQUIRED && f.Kind == KIND_FK) || naturalKey[f.Name])
			if fo != nil && fo.ForceNew != nil {
				if *fo.ForceNew == a.ForceNew {
					add("fields.%s.force_new is a no-op", f.Name)
				}
				a.ForceNew = *fo.ForceNew
			}
		}
		// Validators apply where users write values.
		if a.Writable() && a.Type == TYPE_STRING && a.Encoding == ENC_PLAIN {
			a.MinLength, a.MaxLength = f.MinLength, f.MaxLength
			if f.Kind == KIND_ENUM {
				a.OneOf = append([]string{}, f.Enum...)
			}
		}
		a.Sensitive = f.Type == "Hashed"
		for _, r := range o.Rules.Sensitive {
			if r.matches(t.Name) && r.Field == f.Name {
				usedRules[r] = true
				a.Sensitive = true
			}
		}
		if fo != nil && fo.Sensitive != nil {
			if *fo.Sensitive == a.Sensitive {
				add("fields.%s.sensitive is a no-op", f.Name)
			}
			a.Sensitive = *fo.Sensitive
		}
		for _, r := range o.Rules.VarNames {
			if r.matches(t.Name) && r.Field == f.Name {
				usedRules[r] = true
				if r.Rule == VAR_RULE_ENV {
					a.VarName = cv.EnvVarRule()
				} else {
					a.VarName = cv.ConfigVarRule()
				}
			}
		}
		a.Description = describe(t, a)
		if fo != nil {
			reason := strings.TrimSuffix(strings.TrimSpace(fo.Reason), ".")
			a.Description += " Note: " + reason + "."
		}
	}
	if to.ImportBy != "" {
		f := t.Field(to.ImportBy)
		if f == nil || !f.Unique {
			add("import_by: %s is not a unique column", to.ImportBy)
		} else {
			for _, a := range s.Attributes {
				if a.Field == to.ImportBy && a.Encoding == ENC_PLAIN {
					s.ImportBy = a.Name
				}
			}
		}
	}
	if len(to.Hooks) > 0 {
		s.Hooks = map[string]string{}
		for step, name := range to.Hooks {
			if o.Hooks[name] == nil {
				add("hooks.%s: %s is not declared under hooks", step, name)
			}
			usedHooks[name] = true
			s.Hooks[step] = name
		}
	}
	// Order: id first, then by name.
	sort.SliceStable(s.Attributes, func(i, j int) bool {
		ai, aj := s.Attributes[i], s.Attributes[j]
		if (ai.Name == "id") != (aj.Name == "id") {
			return ai.Name == "id"
		}
		return ai.Name < aj.Name
	})
	s.ResourceDescription, s.DataSourceDescription = describeTable(t, s)
	if len(s.Hooks) > 0 {
		s.ResourceDescription += " " + strings.TrimSpace(to.Reason)
	}
	return s, problems
}

// ruleMode is the resource mode the rules give a column.
func ruleMode(f *Field, a *Attr) string {
	switch {
	case f.Name == "id" || !f.Writable || f.Computed:
		return MODE_COMPUTED
	case a.Encoding == ENC_JSON:
		// JSON values are normalised by the API, so they are read-only until the provider normalises them too.
		return MODE_COMPUTED
	case f.Type == "Boolean":
		// Booleans always have a database default.
		return MODE_OPTIONAL_COMPUTED
	case f.Unique && !f.Nullable:
		// A unique identifier the API generates when it is not given (slug, uuid, handle).
		return MODE_OPTIONAL_COMPUTED
	case f.Nullable && f.Kind == KIND_FK:
		// Nullable references are often maintained by the API (a fleet tracking its latest release).
		return MODE_OPTIONAL_COMPUTED
	case f.Nullable:
		return MODE_OPTIONAL
	default:
		return MODE_REQUIRED
	}
}

// GENERIC_REMAINDERS are words that do not name a value on their own, so a verb is never stripped down to them
// ("is_finalized_at__date" stays "is_finalized_at_date").
var GENERIC_REMAINDERS = map[string]bool{"date": true}

// strippedName drops the leading verb of an SBVR field name ("is_for__device_type" -> "device_type").
func strippedName(field string) string {
	if _, after, ok := strings.Cut(field, "__"); ok && !GENERIC_REMAINDERS[after] {
		return strings.ReplaceAll(after, "__", "_")
	}
	return fullName(field)
}

func fullName(field string) string {
	return strings.ReplaceAll(field, "__", "_")
}

func withSuffix(a *Attr, base string) string {
	if a.Encoding == ENC_REF {
		return base + "_id"
	}
	return base
}

// nameAttributes assigns attribute names. Names in the committed surface lock are sticky; explicit override names
// come next; every other column gets its name with the leading verb stripped, or its full name when the stripped
// name is taken or shared with another new column (unless exactly one of the sharers has no verb, which then keeps
// the stripped name). References get an `_id` suffix unless they are exposed by a column of the referenced row.
func nameAttributes(s *Spec, to *TableOverride, old *SurfaceLock) error {
	taken := map[string]*Attr{}
	assign := func(a *Attr, name string) error {
		if other, ok := taken[name]; ok {
			return fmt.Errorf("attribute name %s is used by both %s and %s; add a name override", name, other.Field, a.Field)
		}
		if TERRAFORM_RESERVED[name] {
			return fmt.Errorf("attribute name %s (%s) is reserved by Terraform; add a name override", name, a.Field)
		}
		a.Name = name
		taken[name] = a
		return nil
	}
	locked := old.names(s.TypeName)
	var pending []*Attr
	for _, a := range s.Attributes {
		if fo := to.Fields[a.Field]; fo != nil && fo.Name != "" {
			if err := assign(a, fo.Name); err != nil {
				return err
			}
			continue
		}
		if a.Field == "id" {
			if err := assign(a, "id"); err != nil {
				return err
			}
			continue
		}
		pending = append(pending, a)
	}
	var fresh []*Attr
	for _, a := range pending {
		if name, ok := locked[a.key()]; ok && taken[name] == nil {
			if err := assign(a, name); err != nil {
				return err
			}
			continue
		}
		fresh = append(fresh, a)
	}
	groups := map[string][]*Attr{}
	fulls := map[string]bool{}
	for _, a := range fresh {
		stripped := withSuffix(a, strippedName(a.Field))
		groups[stripped] = append(groups[stripped], a)
		fulls[withSuffix(a, fullName(a.Field))] = true
	}
	for _, stripped := range sortedKeys(groups) {
		members := groups[stripped]
		winner := (*Attr)(nil)
		if taken[stripped] == nil {
			if len(members) == 1 {
				if full := withSuffix(members[0], fullName(members[0].Field)); full == stripped || !fulls[stripped] {
					winner = members[0]
				}
			} else {
				var verbless []*Attr
				for _, a := range members {
					if withSuffix(a, fullName(a.Field)) == stripped {
						verbless = append(verbless, a)
					}
				}
				if len(verbless) == 1 {
					winner = verbless[0]
				}
			}
		}
		for _, a := range members {
			name := withSuffix(a, fullName(a.Field))
			if a == winner {
				name = stripped
			}
			if err := assign(a, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// humanise turns a resource or field name into words.
func humanise(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, "__", " "), "_", " ")
}

func article(word string) string {
	if word != "" && strings.ContainsRune("aeiou", rune(word[0])) {
		return "an"
	}
	return "a"
}

func describeTable(t *Table, s *Spec) (string, string) {
	what := humanise(t.Name)
	where := fmt.Sprintf("the `%s` resource of the `/%s` API", t.Name, t.Root)
	preview := ""
	if s.Preview {
		preview = " Preview: uses the unversioned /" + t.Root + " API root and may change without notice."
	}
	res := fmt.Sprintf("Manages %s %s (%s).%s", article(what), what, where, preview)
	ds := fmt.Sprintf("Reads the one %s (%s) that matches every configured attribute. At least one attribute must be set, and exactly one row must match.%s",
		what, where, preview)
	return res, ds
}

func describe(t *Table, a *Attr) string {
	f := a.field
	var parts []string
	switch a.Encoding {
	case ENC_REF:
		parts = append(parts, fmt.Sprintf("ID of the referenced `%s` (API field `%s`).", a.RefTable, f.Name))
	case ENC_REF_BY:
		parts = append(parts, fmt.Sprintf("`%s` of the referenced `%s` (API field `%s`, resolved to its ID).", a.RefField, a.RefTable, f.Name))
	case ENC_JSON:
		parts = append(parts, fmt.Sprintf("API field `%s`, as a JSON string.", f.Name))
	default:
		if f.Name == "id" {
			parts = append(parts, fmt.Sprintf("Numeric ID of the %s.", humanise(t.Name)))
		} else {
			parts = append(parts, fmt.Sprintf("API field `%s` (%s).", f.Name, strings.ToLower(f.Type)))
		}
	}
	if len(a.OneOf) > 0 {
		parts = append(parts, "One of "+strings.Join(quoteAll(a.OneOf), ", ")+".")
	}
	switch {
	case a.MinLength > 0 && a.MaxLength > 0 && a.MinLength == a.MaxLength:
		parts = append(parts, fmt.Sprintf("Exactly %d characters.", a.MaxLength))
	case a.MinLength > 0 && a.MaxLength > 0:
		parts = append(parts, fmt.Sprintf("Between %d and %d characters.", a.MinLength, a.MaxLength))
	case a.MaxLength > 0:
		parts = append(parts, fmt.Sprintf("At most %d characters.", a.MaxLength))
	case a.MinLength > 0:
		parts = append(parts, fmt.Sprintf("At least %d characters.", a.MinLength))
	}
	if a.VarName != nil {
		parts = append(parts, "Name rules from the API's /config/vars: "+a.VarName.Describe()+".")
	}
	if a.ResourceMode == MODE_OPTIONAL_COMPUTED {
		parts = append(parts, "The API sets a value when none is configured.")
	}
	if a.ForceNew {
		parts = append(parts, "Changing it replaces the resource.")
	}
	if f.Deprecated() {
		parts = append(parts, "Deprecated upstream.")
	}
	if f.Unique && f.Name != "id" {
		parts = append(parts, "Unique.")
	}
	return strings.Join(parts, " ")
}

func sortStrings(s []string) { sort.Strings(s) }
