package engine

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// ID_ATTRIBUTE is the name of the implicit numeric id attribute.
const ID_ATTRIBUTE = "id"

// resourceSchema builds the resource schema of a descriptor.
func resourceSchema(d Resource) rschema.Schema {
	attrs := map[string]rschema.Attribute{
		ID_ATTRIBUTE: rschema.Int64Attribute{
			Description:   "Numeric identifier assigned by the Balena API.",
			Computed:      true,
			PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
		},
	}
	for _, a := range d.Attributes {
		required := a.Mode == MODE_REQUIRED
		optional := a.Mode == MODE_OPTIONAL || a.Mode == MODE_OPTIONAL_COMPUTED
		computed := a.Mode == MODE_OPTIONAL_COMPUTED || a.Mode == MODE_COMPUTED
		switch a.Type {
		case TYPE_INT64:
			attrs[a.Name] = rschema.Int64Attribute{
				Description: a.Description, Required: required, Optional: optional, Computed: computed, Sensitive: a.Sensitive,
			}
		case TYPE_BOOL:
			attrs[a.Name] = rschema.BoolAttribute{
				Description: a.Description, Required: required, Optional: optional, Computed: computed, Sensitive: a.Sensitive,
			}
		default:
			attrs[a.Name] = rschema.StringAttribute{
				Description: a.Description, Required: required, Optional: optional, Computed: computed, Sensitive: a.Sensitive,
				Validators: enumValidators(a),
			}
		}
	}
	return rschema.Schema{Description: d.Description, Attributes: attrs}
}

// dataSourceSchema builds the data source schema of a descriptor. Filterable
// attributes are lookup arguments; write-only attributes are omitted.
func dataSourceSchema(d Resource) dschema.Schema {
	attrs := map[string]dschema.Attribute{
		ID_ATTRIBUTE: dschema.Int64Attribute{
			Description: "Numeric identifier. Set it to look up by id.",
			Optional:    true,
			Computed:    true,
		},
	}
	for _, a := range dataSourceAttributes(d) {
		optional := a.Filterable
		switch a.Type {
		case TYPE_INT64:
			attrs[a.Name] = dschema.Int64Attribute{
				Description: a.Description, Optional: optional, Computed: true, Sensitive: a.Sensitive,
			}
		case TYPE_BOOL:
			attrs[a.Name] = dschema.BoolAttribute{
				Description: a.Description, Optional: optional, Computed: true, Sensitive: a.Sensitive,
			}
		default:
			attrs[a.Name] = dschema.StringAttribute{
				Description: a.Description, Optional: optional, Computed: true, Sensitive: a.Sensitive,
			}
		}
	}
	return dschema.Schema{Description: d.DataSourceDescription, Attributes: attrs}
}

// dataSourceAttributes returns the attributes a data source exposes.
func dataSourceAttributes(d Resource) []Attribute {
	out := make([]Attribute, 0, len(d.Attributes))
	for _, a := range d.Attributes {
		if !a.WriteOnly {
			out = append(out, a)
		}
	}
	return out
}

func enumValidators(a Attribute) []validator.String {
	if len(a.Enum) == 0 {
		return nil
	}
	return []validator.String{stringvalidator.OneOf(a.Enum...)}
}
