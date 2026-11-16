package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

var (
	_ datasource.DataSource                     = &odataDataSource{}
	_ datasource.DataSourceWithConfigValidators = &odataDataSource{}
)

// odataDataSource is the one data source implementation: it reads the single row of a table that matches every
// configured attribute.
type odataDataSource struct {
	spec   *TableSpec
	client *balena.Client
}

// newODataDataSource returns the data source of a spec.
func newODataDataSource(spec *TableSpec) datasource.DataSource {
	return &odataDataSource{spec: spec}
}

func inDataSource(*AttributeSpec) bool { return true }

func (d *odataDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.spec.TypeSuffix
}

func (d *odataDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := map[string]schema.Attribute{}
	for _, a := range d.spec.Attributes {
		filter := a.DataSource == DS_FILTER
		desc := a.Description
		if filter {
			desc += " Selects the row when configured."
		}
		switch a.Type {
		case ATTR_INT64:
			attrs[a.Name] = schema.Int64Attribute{Description: desc, MarkdownDescription: desc, Optional: filter, Computed: true, Sensitive: a.Sensitive}
		case ATTR_BOOL:
			attrs[a.Name] = schema.BoolAttribute{Description: desc, MarkdownDescription: desc, Optional: filter, Computed: true, Sensitive: a.Sensitive}
		case ATTR_FLOAT64:
			attrs[a.Name] = schema.Float64Attribute{Description: desc, MarkdownDescription: desc, Optional: filter, Computed: true, Sensitive: a.Sensitive}
		default:
			attrs[a.Name] = schema.StringAttribute{Description: desc, MarkdownDescription: desc, Optional: filter, Computed: true, Sensitive: a.Sensitive}
		}
	}
	resp.Schema = schema.Schema{
		Description:         d.spec.DataSourceDescription,
		MarkdownDescription: d.spec.DataSourceDescription,
		Attributes:          attrs,
	}
}

func (d *odataDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	var filters []path.Expression
	for _, a := range d.spec.Attributes {
		if a.DataSource == DS_FILTER {
			filters = append(filters, path.MatchRoot(a.Name))
		}
	}
	return []datasource.ConfigValidator{datasourcevalidator.AtLeastOneOf(filters...)}
}

func (d *odataDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, ok := configureClient(req.ProviderData, &resp.Diagnostics, "Data Source")
	if !ok {
		return
	}
	d.client = client
}

func (d *odataDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	config, diags := getValues(ctx, req.Config, d.spec, inDataSource)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	var filters, described []string
	for _, a := range d.spec.Attributes {
		v := config[a.Name]
		if a.DataSource != DS_FILTER || v.IsNull() || v.IsUnknown() {
			continue
		}
		filters = append(filters, balena.Eq(a.Field, filterValue(v)))
		if a.Sensitive {
			described = append(described, a.Name+" = (sensitive)")
		} else {
			described = append(described, a.Name+" = "+v.String())
		}
	}
	if len(filters) == 0 {
		resp.Diagnostics.AddError("No attribute selects the "+d.spec.TypeSuffix, "Configure at least one attribute that selects the row.")
		return
	}
	q := d.spec.query(inDataSource)
	q.Filter = balena.And(filters...)
	q.Top = 2
	rows, err := d.client.List(ctx, d.spec.Root, d.spec.Table, q)
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+d.spec.TypeSuffix, err.Error())
		return
	}
	switch len(rows) {
	case 0:
		resp.Diagnostics.AddError("No "+d.spec.TypeSuffix+" found", "No row matches "+strings.Join(described, ", ")+".")
		return
	case 1:
	default:
		resp.Diagnostics.AddError("More than one "+d.spec.TypeSuffix+" found",
			fmt.Sprintf("More than one row matches %s; configure more attributes to select exactly one.", strings.Join(described, ", ")))
		return
	}
	values, err := rowValues(d.spec, rows[0], inDataSource)
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+d.spec.TypeSuffix, err.Error())
		return
	}
	resp.Diagnostics.Append(setValues(ctx, &resp.State, values)...)
}
