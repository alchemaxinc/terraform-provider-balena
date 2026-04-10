package provider

import (
	"context"
	"fmt"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &DeviceServiceEnvVarResource{}
	_ resource.ResourceWithImportState = &DeviceServiceEnvVarResource{}
)

// DeviceServiceEnvVarResource implements the balena_device_service_env_var resource.
type DeviceServiceEnvVarResource struct {
	client *balena.Client
}

// DeviceServiceEnvVarResourceModel describes the device service env var data model.
type DeviceServiceEnvVarResourceModel struct {
	ID               types.Int64  `tfsdk:"id"`
	ServiceInstallID types.Int64  `tfsdk:"service_install_id"`
	Name             types.String `tfsdk:"name"`
	Value            types.String `tfsdk:"value"`
}

// NewDeviceServiceEnvVarResource returns a new device service env var resource instance.
func NewDeviceServiceEnvVarResource() resource.Resource {
	return &DeviceServiceEnvVarResource{}
}

func (r *DeviceServiceEnvVarResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_device_service_env_var"
}

func (r *DeviceServiceEnvVarResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a per-service device environment variable.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"service_install_id": schema.Int64Attribute{
				Description: "ID of the service install.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Variable name.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				Description: "Variable value.",
				Required:    true,
				Sensitive:   true,
			},
		},
	}
}

func (r *DeviceServiceEnvVarResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*balena.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *balena.Client")
		return
	}
	r.client = client
}

func (r *DeviceServiceEnvVarResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DeviceServiceEnvVarResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.CreateDeviceServiceEnvVar(ctx, plan.ServiceInstallID.ValueInt64(), plan.Name.ValueString(), plan.Value.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating device service env var", err.Error())
		return
	}

	plan.ID = types.Int64Value(result.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DeviceServiceEnvVarResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DeviceServiceEnvVarResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.GetDeviceServiceEnvVar(ctx, state.ID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading device service env var", err.Error())
		return
	}

	state.ServiceInstallID = types.Int64Value(result.ServiceInstall.ID)
	state.Name = types.StringValue(result.Name)
	state.Value = types.StringValue(result.Value)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *DeviceServiceEnvVarResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan DeviceServiceEnvVarResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state DeviceServiceEnvVarResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.UpdateDeviceServiceEnvVar(ctx, state.ID.ValueInt64(), plan.Value.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error updating device service env var", err.Error())
		return
	}

	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DeviceServiceEnvVarResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DeviceServiceEnvVarResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteDeviceServiceEnvVar(ctx, state.ID.ValueInt64())
	if err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting device service env var", err.Error())
	}
}

func (r *DeviceServiceEnvVarResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric ID, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}
