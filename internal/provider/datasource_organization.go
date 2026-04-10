package provider

import (
	"context"
	"fmt"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource                     = &OrganizationDataSource{}
	_ datasource.DataSourceWithConfigValidators = &OrganizationDataSource{}
)

// OrganizationDataSource implements the balena_organization data source.
type OrganizationDataSource struct {
	client *balena.Client
}

// OrganizationDataSourceModel describes the organization data source data model.
type OrganizationDataSourceModel struct {
	ID     types.Int64  `tfsdk:"id"`
	Name   types.String `tfsdk:"name"`
	Handle types.String `tfsdk:"handle"`
}

// NewOrganizationDataSource returns a new organization data source instance.
func NewOrganizationDataSource() datasource.DataSource {
	return &OrganizationDataSource{}
}

func (d *OrganizationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *OrganizationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a Balena organization by ID or handle.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier of the organization. Provide either id or handle.",
				Optional:    true,
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Name of the organization.",
				Computed:    true,
			},
			"handle": schema.StringAttribute{
				Description: "Handle of the organization. Provide either handle or id.",
				Optional:    true,
				Computed:    true,
			},
		},
	}
}

func (d *OrganizationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*balena.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *balena.Client")
		return
	}
	d.client = client
}

func (d *OrganizationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config OrganizationDataSourceModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var org *balena.Organization
	var err error

	if !config.ID.IsNull() && !config.ID.IsUnknown() {
		org, err = d.client.GetOrganization(ctx, config.ID.ValueInt64())
	} else if !config.Handle.IsNull() && !config.Handle.IsUnknown() {
		org, err = d.client.GetOrganizationByHandle(ctx, config.Handle.ValueString())
	}

	if err != nil {
		if balena.IsNotFound(err) {
			resp.Diagnostics.AddError("Organization not found", fmt.Sprintf("No organization matched the given criteria: %s", err.Error()))
			return
		}
		resp.Diagnostics.AddError("Error reading organization", fmt.Sprintf("Could not read organization: %s", err.Error()))
		return
	}

	config.ID = types.Int64Value(org.ID)
	config.Name = types.StringValue(org.Name)
	config.Handle = types.StringValue(org.Handle)

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}

// ConfigValidators returns validators that ensure exactly one lookup key is provided.
func (d *OrganizationDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(
			path.MatchRoot("id"),
			path.MatchRoot("handle"),
		),
	}
}
