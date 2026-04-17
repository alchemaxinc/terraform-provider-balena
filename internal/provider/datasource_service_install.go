package provider

import (
	"context"
	"fmt"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &ServiceInstallDataSource{}

// ServiceInstallDataSource implements the balena_service_install data source.
type ServiceInstallDataSource struct {
	client *balena.Client
}

// ServiceInstallDataSourceModel describes the data model for a service_install lookup.
type ServiceInstallDataSourceModel struct {
	ID        types.Int64 `tfsdk:"id"`
	DeviceID  types.Int64 `tfsdk:"device_id"`
	ServiceID types.Int64 `tfsdk:"service_id"`
}

// NewServiceInstallDataSource returns a new service_install data source instance.
func NewServiceInstallDataSource() datasource.DataSource {
	return &ServiceInstallDataSource{}
}

func (d *ServiceInstallDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_install"
}

func (d *ServiceInstallDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up the service_install pivot linking a device to a service. Useful when managing balena_device_service_env_var.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier of the service_install.",
				Computed:    true,
			},
			"device_id": schema.Int64Attribute{
				Description: "Numeric identifier of the device.",
				Required:    true,
			},
			"service_id": schema.Int64Attribute{
				Description: "Numeric identifier of the service (see the balena_service data source).",
				Required:    true,
			},
		},
	}
}

func (d *ServiceInstallDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, ok := configureClient(req.ProviderData, &resp.Diagnostics, "Data Source")
	if !ok {
		return
	}
	d.client = client
}

func (d *ServiceInstallDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ServiceInstallDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	si, err := d.client.GetServiceInstall(ctx, config.DeviceID.ValueInt64(), config.ServiceID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.Diagnostics.AddError("service_install not found", fmt.Sprintf("No service_install found for device %d and service %d", config.DeviceID.ValueInt64(), config.ServiceID.ValueInt64()))
			return
		}
		resp.Diagnostics.AddError("Error reading service_install", err.Error())
		return
	}
	config.ID = types.Int64Value(si.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
