package provider

import (
	"context"
	"fmt"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &DeviceProfileOverrideResource{}
	_ resource.ResourceWithImportState = &DeviceProfileOverrideResource{}
)

// DeviceProfileOverrideResource implements the balena_device_profile_override resource.
type DeviceProfileOverrideResource struct {
	client *balena.Client
}

// DeviceProfileOverrideResourceModel describes the device profile override data model.
type DeviceProfileOverrideResourceModel struct {
	ID                types.Int64  `tfsdk:"id"`
	DeviceID          types.Int64  `tfsdk:"device_id"`
	ProfileName       types.String `tfsdk:"profile_name"`
	HostApplicationID types.Int64  `tfsdk:"host_application_id"`
	IsActive          types.Bool   `tfsdk:"is_active"`
}

// NewDeviceProfileOverrideResource returns a new device profile override resource instance.
func NewDeviceProfileOverrideResource() resource.Resource {
	return &DeviceProfileOverrideResource{}
}

func (r *DeviceProfileOverrideResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_device_profile_override"
}

func (r *DeviceProfileOverrideResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Overrides the profile of a host OS application on a single device.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier assigned by the Balena API.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"device_id": schema.Int64Attribute{
				Description: "Numeric ID of the device overriding the profile. The device must run a release belonging to the host OS application.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"profile_name": schema.StringAttribute{
				Description: "Profile name, between 2 and 100 characters.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{profileNameValidator},
			},
			"host_application_id": schema.Int64Attribute{
				Description: "Numeric ID of the host OS application that provides the profile. Must be a host application whose class is not \"block\".",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"is_active": schema.BoolAttribute{
				Description: "Whether the override is active. Defaults to true.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
		},
	}
}

func (r *DeviceProfileOverrideResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := configureClient(req.ProviderData, &resp.Diagnostics, "Resource")
	if !ok {
		return
	}
	r.client = client
}

func (r *DeviceProfileOverrideResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DeviceProfileOverrideResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	isActive := plan.IsActive.ValueBool()
	result, err := r.client.CreateDeviceProfileOverride(ctx, plan.DeviceID.ValueInt64(), plan.ProfileName.ValueString(), plan.HostApplicationID.ValueInt64(), &isActive)
	if err != nil {
		resp.Diagnostics.AddError("Error creating device profile override", err.Error())
		return
	}

	plan.ID = types.Int64Value(result.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DeviceProfileOverrideResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DeviceProfileOverrideResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.GetDeviceProfileOverride(ctx, state.ID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading device profile override", err.Error())
		return
	}

	state.DeviceID = types.Int64Value(result.Device.ID)
	state.ProfileName = types.StringValue(result.ProfileName)
	state.HostApplicationID = types.Int64Value(result.HostApp.ID)
	state.IsActive = types.BoolValue(result.IsActive)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *DeviceProfileOverrideResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state DeviceProfileOverrideResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID
	if !plan.IsActive.Equal(state.IsActive) {
		if err := r.client.UpdateDeviceProfileOverride(ctx, plan.ID.ValueInt64(), plan.IsActive.ValueBool()); err != nil {
			resp.Diagnostics.AddError("Error updating device profile override", err.Error())
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DeviceProfileOverrideResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DeviceProfileOverrideResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteDeviceProfileOverride(ctx, state.ID.ValueInt64()); err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting device profile override", err.Error())
	}
}

func (r *DeviceProfileOverrideResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric ID, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}
