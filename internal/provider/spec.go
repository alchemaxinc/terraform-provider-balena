package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/attr"
)

// AttrType is the Terraform value type of an attribute.
type AttrType int

// Attribute value types.
const (
	ATTR_STRING AttrType = iota
	ATTR_INT64
	ATTR_BOOL
	ATTR_FLOAT64
)

// Encoding is how an attribute value maps to its API field.
type Encoding int

const (
	// ENC_PLAIN is a scalar column.
	ENC_PLAIN Encoding = iota
	// ENC_JSON is a JSON column rendered as a JSON string (read-only).
	ENC_JSON
	// ENC_REF is a reference rendered as the referenced id: {"__id": N} on read, N on write.
	ENC_REF
	// ENC_REF_BY is a reference rendered as a column (RefField) of the referenced row: $expand on read, resolved to an
	// id on write.
	ENC_REF_BY
)

// Mode is how a resource attribute is set.
type Mode int

// Resource attribute modes.
const (
	MODE_NONE Mode = iota
	MODE_REQUIRED
	MODE_OPTIONAL
	MODE_COMPUTED
	MODE_OPTIONAL_COMPUTED
)

// DataSourceMode is how a data source attribute is set.
type DataSourceMode int

// Data source attribute modes.
const (
	// DS_FILTER attributes may be configured to select the row, and are read back otherwise.
	DS_FILTER DataSourceMode = iota
	// DS_COMPUTED attributes are only read.
	DS_COMPUTED
)

// TableSpec is the generated description of one API table and its Terraform resource and data source.
type TableSpec struct {
	// TypeSuffix is the Terraform type name without the provider prefix.
	TypeSuffix            string
	Table                 string
	Root                  string
	Preview               bool
	Resource              bool
	DataSource            bool
	ResourceDescription   string
	DataSourceDescription string
	// ImportBy names the attribute an import ID is matched against; empty imports by numeric id.
	ImportBy   string
	Hooks      TableHooks
	Attributes []*AttributeSpec
}

// AttributeSpec describes one attribute.
type AttributeSpec struct {
	Name     string
	Field    string
	Type     AttrType
	Encoding Encoding
	RefTable string
	RefField string
	// Resolver resolves an ENC_REF_BY value to an id when RefField is not unique on its own.
	Resolver   ResolverHook
	Resource   Mode
	DataSource DataSourceMode
	ForceNew   bool
	Sensitive  bool
	// Stable marks a value that never changes once set, so plans keep the prior value.
	Stable bool
	// DateTime marks an RFC 3339 date-time: values that name the same instant are equal, whatever their notation.
	DateTime    bool
	MinLength   int
	MaxLength   int
	OneOf       []string
	VarName     *VarNameRule
	Description string
}

// writable reports whether the resource lets users set the attribute.
func (a *AttributeSpec) writable() bool {
	return a.Resource == MODE_REQUIRED || a.Resource == MODE_OPTIONAL || a.Resource == MODE_OPTIONAL_COMPUTED
}

// VarNameRule is a variable-name validator derived from the API's /config/vars.
type VarNameRule struct {
	Invalid           string
	Reserved          []string
	RequiredPrefixes  []string
	ForbiddenPrefixes []string
}

// HookContext gives hooks the client and the spec of the table they serve.
type HookContext struct {
	Client *balena.Client
	Spec   *TableSpec
}

// TableHooks replace CRUD steps with hand-written behaviour. Every hook is named in schema/overrides.yaml, declares
// the API columns it uses there, and is checked against the IR by the generator.
type TableHooks struct {
	// Create returns the id of the row the new resource represents instead of creating one. The runtime then
	// patches the row to the planned values.
	Create CreateHook
	// Delete replaces DELETE.
	Delete DeleteHook
}

// CreateHook finds the row a new resource represents. planned holds the planned attribute values by name.
type CreateHook func(ctx context.Context, hc *HookContext, planned map[string]attr.Value) (int64, error)

// DeleteHook removes (or forgets) a row.
type DeleteHook func(ctx context.Context, hc *HookContext, id int64) error

// ResolverHook resolves the value of an ENC_REF_BY attribute to the id of the referenced row, for the row with the
// given id.
type ResolverHook func(ctx context.Context, hc *HookContext, id int64, value string) (int64, error)

// attr returns the named attribute.
func (s *TableSpec) attr(name string) *AttributeSpec {
	for _, a := range s.Attributes {
		if a.Name == name {
			return a
		}
	}
	return nil
}

// query returns the $select and $expand that read every attribute for which include returns true.
func (s *TableSpec) query(include func(*AttributeSpec) bool) balena.Query {
	var q balena.Query
	selected := map[string]bool{}
	for _, a := range s.Attributes {
		if !include(a) {
			continue
		}
		if a.Encoding == ENC_REF_BY {
			q.Expand = append(q.Expand, balena.Expand{Field: a.Field, Select: []string{a.RefField}})
			continue
		}
		if !selected[a.Field] {
			selected[a.Field] = true
			q.Select = append(q.Select, a.Field)
		}
	}
	return q
}
