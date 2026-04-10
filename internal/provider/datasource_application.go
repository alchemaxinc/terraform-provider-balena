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
	_ datasource.DataSource                     = &ApplicationDataSource{}
	_ datasource.DataSourceWithConfigValidators = &ApplicationDataSource{}
)

// ApplicationDataSource implements the balena_application data source.
type ApplicationDataSource struct {
	client *balena.Client
}

// ApplicationDataSourceModel describes the application data source data model.
type ApplicationDataSourceModel struct {
	ID             types.Int64  `tfsdk:"id"`
	AppName        types.String `tfsdk:"app_name"`
	Slug           types.String `tfsdk:"slug"`
	DeviceType     types.String `tfsdk:"device_type"`
	OrganizationID types.Int64  `tfsdk:"organization_id"`
	IsPublic       types.Bool   `tfsdk:"is_public"`
	IsArchived     types.Bool   `tfsdk:"is_archived"`
}

// NewApplicationDataSource returns a new application data source instance.
func NewApplicationDataSource() datasource.DataSource {
	return &ApplicationDataSource{}
}

func (d *ApplicationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application"
}

func (d *ApplicationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a Balena application by name or ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier of the application. Provide either id or app_name.",
				Optional:    true,
				Computed:    true,
			},
			"app_name": schema.StringAttribute{
				Description: "Name of the application. Provide either app_name or id.",
				Optional:    true,
				Computed:    true,
			},
			"slug": schema.StringAttribute{
				Description: "Slug of the application.",
				Computed:    true,
			},
			"device_type": schema.StringAttribute{
				Description: "Device type.",
				Computed:    true,
			},
			"organization_id": schema.Int64Attribute{
				Description: "Organization ID.",
				Computed:    true,
			},
			"is_public": schema.BoolAttribute{
				Description: "Whether the application is public.",
				Computed:    true,
			},
			"is_archived": schema.BoolAttribute{
				Description: "Whether the application is archived.",
				Computed:    true,
			},
		},
	}
}

func (d *ApplicationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ApplicationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ApplicationDataSourceModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var app *balena.Application
	var err error

	if !config.ID.IsNull() && !config.ID.IsUnknown() {
		app, err = d.client.GetApplication(ctx, config.ID.ValueInt64())
	} else if !config.AppName.IsNull() && !config.AppName.IsUnknown() {
		app, err = d.client.GetApplicationByName(ctx, config.AppName.ValueString())
	}

	if err != nil {
		if balena.IsNotFound(err) {
			resp.Diagnostics.AddError("Application not found", fmt.Sprintf("No application matched the given criteria: %s", err.Error()))
			return
		}
		resp.Diagnostics.AddError("Error reading application", fmt.Sprintf("Could not read application: %s", err.Error()))
		return
	}

	config.ID = types.Int64Value(app.ID)
	config.AppName = types.StringValue(app.AppName)
	config.Slug = types.StringValue(app.Slug)
	config.DeviceType = types.StringValue(app.DeviceTypeSlug())
	config.OrganizationID = types.Int64Value(app.Org.ID)
	config.IsPublic = types.BoolValue(app.IsPublic)
	config.IsArchived = types.BoolValue(app.IsArchived)

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}

// ConfigValidators returns validators that ensure exactly one lookup key is provided.
func (d *ApplicationDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(
			path.MatchRoot("id"),
			path.MatchRoot("app_name"),
		),
	}
}
