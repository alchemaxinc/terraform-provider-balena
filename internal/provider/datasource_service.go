package provider

import (
	"context"
	"fmt"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &ServiceDataSource{}

// ServiceDataSource implements the balena_service data source.
type ServiceDataSource struct {
	client *balena.Client
}

// ServiceDataSourceModel describes the service data source data model.
type ServiceDataSourceModel struct {
	ID            types.Int64  `tfsdk:"id"`
	ApplicationID types.Int64  `tfsdk:"application_id"`
	ServiceName   types.String `tfsdk:"service_name"`
}

// NewServiceDataSource returns a new service data source instance.
func NewServiceDataSource() datasource.DataSource {
	return &ServiceDataSource{}
}

func (d *ServiceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service"
}

func (d *ServiceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a Balena service by application ID and service name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier of the service.",
				Computed:    true,
			},
			"application_id": schema.Int64Attribute{
				Description: "ID of the application the service belongs to.",
				Required:    true,
			},
			"service_name": schema.StringAttribute{
				Description: "Name of the service.",
				Required:    true,
			},
		},
	}
}

func (d *ServiceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ServiceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ServiceDataSourceModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	svc, err := d.client.GetServiceByName(ctx, config.ApplicationID.ValueInt64(), config.ServiceName.ValueString())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.Diagnostics.AddError("Service not found", fmt.Sprintf("No service found with name %q in application %d: %s", config.ServiceName.ValueString(), config.ApplicationID.ValueInt64(), err.Error()))
			return
		}
		resp.Diagnostics.AddError("Error reading service", fmt.Sprintf("Could not read service: %s", err.Error()))
		return
	}

	config.ID = types.Int64Value(svc.ID)

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
