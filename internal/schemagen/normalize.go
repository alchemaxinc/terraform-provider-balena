package schemagen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// WIRE_NAME_PATTERN is what a resource or field wire name must match. Names
// flow into Terraform type and attribute names, Go source and Markdown, so
// anything else fails normalization.
var WIRE_NAME_PATTERN = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// MAX_RAW_CONFIG_BYTES bounds the size of the raw model config accepted by Normalize.
const MAX_RAW_CONFIG_BYTES = 64 << 20

// SQL_TYPES maps PineJS SBVR data types to normalized field types.
var SQL_TYPES = map[string]FieldType{
	"Serial":              TYPE_INTEGER,
	"Big Serial":          TYPE_INTEGER,
	"Integer":             TYPE_INTEGER,
	"Big Integer":         TYPE_INTEGER,
	"Numeric Big Integer": TYPE_INTEGER,
	"ConceptType":         TYPE_INTEGER,
	"ForeignKey":          TYPE_FOREIGN_KEY,
	"Short Text":          TYPE_STRING,
	"Text":                TYPE_STRING,
	"Hashed":              TYPE_HASHED,
	"Boolean":             TYPE_BOOLEAN,
	"Date Time":           TYPE_DATE_TIME,
	"JSON":                TYPE_JSON,
}

// SERIAL_TYPES are SBVR data types whose value the database assigns.
var SERIAL_TYPES = map[string]bool{"Serial": true, "Big Serial": true}

// rawConfig is the subset of the open-balena-api model config that normalization reads.
type rawConfig struct {
	Models []rawModel `json:"models"`
}

type rawModel struct {
	APIRoot      string                                `json:"apiRoot"`
	ModelName    string                                `json:"modelName"`
	TranslateTo  string                                `json:"translateTo"`
	AbstractSQL  rawAbstractSQL                        `json:"abstractSql"`
	Translations map[string]map[string]json.RawMessage `json:"translations"`
}

type rawAbstractSQL struct {
	Tables   map[string]rawTable `json:"tables"`
	Synonyms map[string]string   `json:"synonyms"`
}

type rawTable struct {
	Name    string     `json:"name"`
	IDField string     `json:"idField"`
	Fields  []rawField `json:"fields"`
}

type rawField struct {
	FieldName    string            `json:"fieldName"`
	DataType     string            `json:"dataType"`
	Required     bool              `json:"required"`
	DefaultValue json.RawMessage   `json:"defaultValue"`
	References   *rawReference     `json:"references"`
	Checks       []json.RawMessage `json:"checks"`
	Computed     json.RawMessage   `json:"computed"`
}

type rawReference struct {
	ResourceName string `json:"resourceName"`
}

func (t *rawTable) field(name string) *rawField {
	for i := range t.Fields {
		if t.Fields[i].FieldName == name {
			return &t.Fields[i]
		}
	}
	return nil
}

func isNullJSON(m json.RawMessage) bool {
	m = bytes.TrimSpace(m)
	return len(m) == 0 || bytes.Equal(m, []byte("null"))
}

// SQLNameToODataName mirrors PineJS: `-` becomes `__` and a space becomes `_`.
func SQLNameToODataName(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "-", "__"), " ", "_")
}

// odataNameToSQLName mirrors PineJS: `__` becomes `-` and `_` becomes a space.
func odataNameToSQLName(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "__", "-"), "_", " ")
}

// resolveResource resolves a wire resource name to a table key the way the
// PineJS router does: each `-` separated part goes through the synonyms.
func resolveResource(synonyms map[string]string, wire string) string {
	parts := strings.Split(odataNameToSQLName(wire), "-")
	for i, p := range parts {
		if canonical, ok := synonyms[p]; ok {
			parts[i] = canonical
		}
	}
	return strings.Join(parts, "-")
}

// Normalize reduces the raw model config to the spec of one API root.
func Normalize(raw []byte, apiRoot string) (*Spec, error) {
	if len(raw) > MAX_RAW_CONFIG_BYTES {
		return nil, fmt.Errorf("raw config is %d bytes, limit %d", len(raw), MAX_RAW_CONFIG_BYTES)
	}
	var cfg rawConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("decoding raw config: %w", err)
	}
	byRoot := map[string]*rawModel{}
	for i := range cfg.Models {
		m := &cfg.Models[i]
		if _, dup := byRoot[m.APIRoot]; dup {
			return nil, fmt.Errorf("duplicate apiRoot %q", m.APIRoot)
		}
		byRoot[m.APIRoot] = m
	}
	model, ok := byRoot[apiRoot]
	if !ok {
		return nil, fmt.Errorf("no model with apiRoot %q", apiRoot)
	}
	n := &normalizer{byRoot: byRoot, model: model}
	return n.run()
}

type normalizer struct {
	byRoot map[string]*rawModel
	model  *rawModel
	// wireNames maps table keys of the selected model to their wire names.
	wireNames map[string]string
}

func (n *normalizer) run() (*Spec, error) {
	tables := n.model.AbstractSQL.Tables
	keys := make([]string, 0, len(tables))
	for k := range tables {
		if !strings.Contains(k, "$") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	n.wireNames = map[string]string{}
	seen := map[string]string{}
	for _, k := range keys {
		wire, err := n.wireName(k, tables[k])
		if err != nil {
			return nil, err
		}
		if other, dup := seen[wire]; dup {
			return nil, fmt.Errorf("tables %q and %q share wire name %q", other, k, wire)
		}
		seen[wire] = k
		n.wireNames[k] = wire
	}

	spec := &Spec{Format: SPEC_FORMAT, APIRoot: n.model.APIRoot}
	for _, k := range keys {
		r, err := n.resource(k, tables[k])
		if err != nil {
			return nil, fmt.Errorf("table %q: %w", k, err)
		}
		spec.Resources = append(spec.Resources, r)
	}
	sort.Slice(spec.Resources, func(i, j int) bool { return spec.Resources[i].Name < spec.Resources[j].Name })
	return spec, nil
}

// wireName picks the name the router serves the table under: the OData form of
// the table name, else of the key, whichever resolves back to the key.
func (n *normalizer) wireName(key string, t rawTable) (string, error) {
	for _, candidate := range []string{SQLNameToODataName(t.Name), SQLNameToODataName(key)} {
		if resolveResource(n.model.AbstractSQL.Synonyms, candidate) == key {
			if !WIRE_NAME_PATTERN.MatchString(candidate) {
				return "", fmt.Errorf("table %q: wire name %q does not match %s", key, candidate, WIRE_NAME_PATTERN)
			}
			return candidate, nil
		}
	}
	return "", fmt.Errorf("table %q (name %q): no wire name resolves back to the key", key, t.Name)
}

func (n *normalizer) resource(key string, t rawTable) (Resource, error) {
	if SQLNameToODataName(t.IDField) != "id" {
		return Resource{}, fmt.Errorf("id field is %q, want %q", t.IDField, "id")
	}
	r := Resource{Name: n.wireNames[key], Term: t.Name, Key: key}
	for _, f := range t.Fields {
		typ, ok := SQL_TYPES[f.DataType]
		if !ok {
			r.Unsupported = append(r.Unsupported, UnsupportedField{Name: SQLNameToODataName(f.FieldName), SQLType: f.DataType})
			continue
		}
		readOnly, err := n.readOnly(n.model, key, f.FieldName, 0)
		if err != nil {
			return Resource{}, fmt.Errorf("field %q: %w", f.FieldName, err)
		}
		field := Field{
			Name:          SQLNameToODataName(f.FieldName),
			Term:          f.FieldName,
			Type:          typ,
			SQLType:       f.DataType,
			Required:      f.Required,
			ServerDefault: !isNullJSON(f.DefaultValue) || SERIAL_TYPES[f.DataType],
			ReadOnly:      readOnly,
			Enum:          enumValues(f),
		}
		if typ == TYPE_FOREIGN_KEY {
			if f.References == nil {
				return Resource{}, fmt.Errorf("foreign key %q has no reference", f.FieldName)
			}
			target, ok := n.wireNames[f.References.ResourceName]
			if !ok {
				return Resource{}, fmt.Errorf("foreign key %q references unknown table %q", f.FieldName, f.References.ResourceName)
			}
			field.References = target
		}
		if !WIRE_NAME_PATTERN.MatchString(field.Name) {
			return Resource{}, fmt.Errorf("field wire name %q does not match %s", field.Name, WIRE_NAME_PATTERN)
		}
		if r.Field(field.Name) != nil {
			return Resource{}, fmt.Errorf("duplicate field wire name %q", field.Name)
		}
		r.Fields = append(r.Fields, field)
	}
	if r.Field("id") == nil {
		return Resource{}, fmt.Errorf("no supported id field")
	}
	sort.Slice(r.Fields, func(i, j int) bool { return r.Fields[i].Name < r.Fields[j].Name })
	return r, nil
}

// MAX_TRANSLATION_DEPTH bounds the walk down the translation chain.
const MAX_TRANSLATION_DEPTH = 8

// readOnly follows a field down the translation chain to the base model. A
// field is read-only when any hop computes it rather than mapping it to a
// field of the next model.
func (n *normalizer) readOnly(m *rawModel, tableKey, fieldName string, depth int) (bool, error) {
	if depth > MAX_TRANSLATION_DEPTH {
		return false, fmt.Errorf("translation chain deeper than %d", MAX_TRANSLATION_DEPTH)
	}
	t, ok := m.AbstractSQL.Tables[tableKey]
	if !ok {
		return false, fmt.Errorf("model %q has no table %q", m.APIRoot, tableKey)
	}
	f := t.field(fieldName)
	if f == nil {
		// A field missing from the next model cannot be written through.
		return true, nil
	}
	if !isNullJSON(f.Computed) {
		return true, nil
	}
	if m.TranslateTo == "" {
		return false, nil
	}
	next, ok := n.byRoot[m.TranslateTo]
	if !ok {
		return false, fmt.Errorf("model %q translates to unknown apiRoot %q", m.APIRoot, m.TranslateTo)
	}

	nextKey, nextField := tableKey, fieldName
	if def, ok := m.Translations[tableKey]; ok {
		if raw, ok := def["$toResource"]; ok {
			if err := json.Unmarshal(raw, &nextKey); err != nil {
				return false, fmt.Errorf("table %q: $toResource is not a string", tableKey)
			}
		}
		if alias, ok := def[fieldName]; ok {
			var renamed string
			if err := json.Unmarshal(alias, &renamed); err != nil {
				// Not a plain rename: the translation computes the field.
				return true, nil
			}
			nextField = renamed
		}
	}
	if _, ok := next.AbstractSQL.Tables[nextKey]; !ok {
		return true, nil
	}
	return n.readOnly(next, nextKey, nextField, depth+1)
}

// enumValues extracts the allowed values of an `In` check on the field itself.
func enumValues(f rawField) []string {
	for _, raw := range f.Checks {
		var check []json.RawMessage
		if json.Unmarshal(raw, &check) != nil || len(check) < 3 {
			continue
		}
		var op string
		if json.Unmarshal(check[0], &op) != nil || op != "In" {
			continue
		}
		var subject []string
		if json.Unmarshal(check[1], &subject) != nil || len(subject) != 2 || subject[0] != "Field" || subject[1] != f.FieldName {
			continue
		}
		var values []string
		for _, rawValue := range check[2:] {
			var v []string
			if json.Unmarshal(rawValue, &v) != nil || len(v) != 2 || v[0] != "Text" {
				values = nil
				break
			}
			values = append(values, v[1])
		}
		if len(values) > 0 {
			return values
		}
	}
	return nil
}
