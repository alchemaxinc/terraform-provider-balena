package provider

import (
	"context"
	"fmt"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &DeviceDataSource{}

// DeviceDataSource implements the balena_device data source.
type DeviceDataSource struct {
	client *balena.Client
}

// DeviceDataSourceModel describes the device data source data model.
type DeviceDataSourceModel struct {
	UUID              types.String `tfsdk:"uuid"`
	ID                types.Int64  `tfsdk:"id"`
	DeviceName        types.String `tfsdk:"device_name"`
	ApplicationID     types.Int64  `tfsdk:"application_id"`
	DeviceType        types.String `tfsdk:"device_type"`
	Status            types.String `tfsdk:"status"`
	IsOnline          types.Bool   `tfsdk:"is_online"`
	IPAddress         types.String `tfsdk:"ip_address"`
	OSVersion         types.String `tfsdk:"os_version"`
	SupervisorVersion types.String `tfsdk:"supervisor_version"`
}

// NewDeviceDataSource returns a new device data source instance.
func NewDeviceDataSource() datasource.DataSource {
	return &DeviceDataSource{}
}

func (d *DeviceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_device"
}

func (d *DeviceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a Balena device by UUID.",
		Attributes: map[string]schema.Attribute{
			"uuid": schema.StringAttribute{
				Description: "UUID of the device.",
				Required:    true,
			},
			"id": schema.Int64Attribute{
				Description: "Numeric identifier.",
				Computed:    true,
			},
			"device_name": schema.StringAttribute{
				Description: "Name of the device.",
				Computed:    true,
			},
			"application_id": schema.Int64Attribute{
				Description: "Application this device belongs to.",
				Computed:    true,
			},
			"device_type": schema.StringAttribute{
				Description: "Device type.",
				Computed:    true,
			},
			"status": schema.StringAttribute{
				Description: "Device status.",
				Computed:    true,
			},
			"is_online": schema.BoolAttribute{
				Description: "Whether the device is online.",
				Computed:    true,
			},
			"ip_address": schema.StringAttribute{
				Description: "IP address.",
				Computed:    true,
			},
			"os_version": schema.StringAttribute{
				Description: "OS version.",
				Computed:    true,
			},
			"supervisor_version": schema.StringAttribute{
				Description: "Supervisor version.",
				Computed:    true,
			},
		},
	}
}

func (d *DeviceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *DeviceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config DeviceDataSourceModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	device, err := d.client.GetDeviceByUUID(ctx, config.UUID.ValueString())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.Diagnostics.AddError("Device not found", fmt.Sprintf("No device found with UUID %q: %s", config.UUID.ValueString(), err.Error()))
			return
		}
		resp.Diagnostics.AddError("Error reading device", fmt.Sprintf("Could not read device: %s", err.Error()))
		return
	}

	config.ID = types.Int64Value(device.ID)
	config.DeviceName = types.StringValue(device.DeviceName)
	config.ApplicationID = types.Int64Value(device.BelongsToApp.ID)
	config.DeviceType = types.StringValue(device.DeviceTypeSlug())
	config.Status = types.StringValue(device.Status)
	config.IsOnline = types.BoolValue(device.IsOnline)
	config.IPAddress = types.StringValue(device.IPAddress)
	config.OSVersion = types.StringValue(device.OSVersion)
	config.SupervisorVersion = types.StringValue(device.SupervisorVersion)

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
