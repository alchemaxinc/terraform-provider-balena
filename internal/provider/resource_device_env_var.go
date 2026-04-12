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
	_ resource.Resource                = &DeviceEnvVarResource{}
	_ resource.ResourceWithImportState = &DeviceEnvVarResource{}
)

// DeviceEnvVarResource implements the balena_device_env_var resource.
type DeviceEnvVarResource struct {
	client *balena.Client
}

// DeviceEnvVarResourceModel describes the device env var data model.
type DeviceEnvVarResourceModel struct {
	ID       types.Int64  `tfsdk:"id"`
	DeviceID types.Int64  `tfsdk:"device_id"`
	Name     types.String `tfsdk:"name"`
	Value    types.String `tfsdk:"value"`
}

// NewDeviceEnvVarResource returns a new device env var resource instance.
func NewDeviceEnvVarResource() resource.Resource {
	return &DeviceEnvVarResource{}
}

func (r *DeviceEnvVarResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_device_env_var"
}

func (r *DeviceEnvVarResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a device-level environment variable.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"device_id": schema.Int64Attribute{
				Description: "ID of the device.",
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

func (r *DeviceEnvVarResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *DeviceEnvVarResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DeviceEnvVarResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.CreateDeviceEnvVar(ctx, plan.DeviceID.ValueInt64(), plan.Name.ValueString(), plan.Value.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating device env var", err.Error())
		return
	}

	plan.ID = types.Int64Value(result.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DeviceEnvVarResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DeviceEnvVarResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.GetDeviceEnvVar(ctx, state.ID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading device env var", err.Error())
		return
	}

	state.DeviceID = types.Int64Value(result.Device.ID)
	state.Name = types.StringValue(result.Name)
	state.Value = types.StringValue(result.Value)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *DeviceEnvVarResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan DeviceEnvVarResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state DeviceEnvVarResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.UpdateDeviceEnvVar(ctx, state.ID.ValueInt64(), plan.Value.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error updating device env var", err.Error())
		return
	}

	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DeviceEnvVarResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DeviceEnvVarResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteDeviceEnvVar(ctx, state.ID.ValueInt64())
	if err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting device env var", err.Error())
	}
}

func (r *DeviceEnvVarResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric ID, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}
