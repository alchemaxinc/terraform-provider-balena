// Package engine implements Terraform resources and data sources generically
// from generated descriptors. It knows the Pine.js wire format and the
// Terraform plugin framework, but no Balena resource names: those come from the
// descriptors in internal/generated.
package engine

// AttrType is the Terraform type of an attribute.
type AttrType int

// Terraform attribute types.
const (
	TYPE_INT64 AttrType = iota
	TYPE_STRING
	TYPE_BOOL
)

// Codec selects how an attribute value maps to and from the Pine.js wire format.
type Codec int

// Wire codecs.
const (
	// CODEC_PLAIN maps the JSON value directly. Integers may arrive as JSON
	// numbers or strings.
	CODEC_PLAIN Codec = iota
	// CODEC_FOREIGN_KEY reads {"__id": N} and writes a bare numeric id.
	CODEC_FOREIGN_KEY
	// CODEC_JSON holds a JSON document as a string attribute.
	CODEC_JSON
)

// Mode says who sets an attribute's value.
type Mode int

// Attribute modes.
const (
	// MODE_REQUIRED attributes must be set in configuration.
	MODE_REQUIRED Mode = iota
	// MODE_OPTIONAL attributes may be set; removing one clears it.
	MODE_OPTIONAL
	// MODE_OPTIONAL_COMPUTED attributes may be set; otherwise the server fills them.
	MODE_OPTIONAL_COMPUTED
	// MODE_COMPUTED attributes are only ever set by the server.
	MODE_COMPUTED
)

// Attribute describes one attribute of a resource and its wire field.
type Attribute struct {
	// Name is the Terraform attribute name.
	Name string
	// Wire is the JSON field name used in requests and OData queries.
	Wire  string
	Type  AttrType
	Codec Codec
	Mode  Mode
	// Sensitive hides the value in plans and logs.
	Sensitive bool
	// WriteOnly attributes are never read back: the API does not return the
	// written value.
	WriteOnly bool
	// Filterable attributes can be used as data source lookup arguments.
	Filterable bool
	// Enum lists the allowed values of a string attribute.
	Enum        []string
	Description string
}

// Resource describes one API collection exposed as a resource and a data source.
// The numeric id attribute is implicit.
type Resource struct {
	// TypeName is the Terraform type name without the provider prefix.
	TypeName string
	// Path is the collection path, for example /v6/application.
	Path string
	// Description is the resource's documentation text.
	Description string
	// DataSourceDescription is the data source's documentation text.
	DataSourceDescription string
	Attributes            []Attribute
}

// writable reports whether the attribute is sent to the API.
func (a Attribute) writable() bool {
	return a.Mode != MODE_COMPUTED
}
