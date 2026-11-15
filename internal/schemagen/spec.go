// Package schemagen turns the upstream open-balena-api model into the provider's
// normalized API spec, and the spec into generated Terraform descriptors.
//
// The package knows PineJS and SBVR semantics only. It contains no Balena
// resource names: everything specific to Balena comes from the spec or from the
// override layer.
package schemagen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// SPEC_FORMAT is the version of the normalized spec format.
const SPEC_FORMAT = 1

// FieldType is the normalized type of a field.
type FieldType string

// Normalized field types.
const (
	TYPE_INTEGER     FieldType = "integer"
	TYPE_FOREIGN_KEY FieldType = "foreign_key"
	TYPE_STRING      FieldType = "string"
	TYPE_HASHED      FieldType = "hashed"
	TYPE_BOOLEAN     FieldType = "boolean"
	TYPE_DATE_TIME   FieldType = "date_time"
	TYPE_JSON        FieldType = "json"
)

// Spec is the normalized model of one API root, as the provider consumes it.
type Spec struct {
	Format    int        `json:"format"`
	APIRoot   string     `json:"api_root"`
	Resources []Resource `json:"resources"`
}

// Resource is one served resource (an OData entity set).
type Resource struct {
	// Name is the wire name used in request paths.
	Name string `json:"name"`
	// Term is the SBVR term of the table, used for descriptions.
	Term string `json:"term"`
	// Key is the table key in the abstract SQL model.
	Key    string  `json:"key"`
	Fields []Field `json:"fields"`
	// Unsupported lists fields whose type the spec format cannot represent.
	Unsupported []UnsupportedField `json:"unsupported,omitempty"`
}

// Field is one field of a resource. The id field is included.
type Field struct {
	// Name is the wire name used in JSON bodies and OData queries.
	Name string `json:"name"`
	// Term is the SBVR field name, used for descriptions.
	Term     string    `json:"term"`
	Type     FieldType `json:"type"`
	SQLType  string    `json:"sql_type"`
	Required bool      `json:"required"`
	// ServerDefault is true when the server fills the field when it is omitted.
	ServerDefault bool `json:"server_default,omitempty"`
	// ReadOnly is true when writes cannot reach the field: a computed field
	// somewhere along the translation chain.
	ReadOnly bool `json:"read_only,omitempty"`
	// References is the wire name of the resource a foreign key points to.
	References string   `json:"references,omitempty"`
	Enum       []string `json:"enum,omitempty"`
}

// UnsupportedField records a field skipped by normalization.
type UnsupportedField struct {
	Name    string `json:"name"`
	SQLType string `json:"sql_type"`
}

// Field returns the named field, or nil.
func (r *Resource) Field(name string) *Field {
	for i := range r.Fields {
		if r.Fields[i].Name == name {
			return &r.Fields[i]
		}
	}
	return nil
}

// Resource returns the named resource, or nil.
func (s *Spec) Resource(name string) *Resource {
	for i := range s.Resources {
		if s.Resources[i].Name == name {
			return &s.Resources[i]
		}
	}
	return nil
}

// MarshalSpec encodes a spec deterministically, with a trailing newline.
func MarshalSpec(s *Spec) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// LoadSpec reads and strictly decodes a spec file.
func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s Spec
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	if s.Format != SPEC_FORMAT {
		return nil, fmt.Errorf("%s: spec format %d, want %d", path, s.Format, SPEC_FORMAT)
	}
	return &s, nil
}
