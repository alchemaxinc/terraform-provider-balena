package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &DeviceResource{}
	_ resource.ResourceWithImportState = &DeviceResource{}
)

// DeviceResource implements the balena_device resource.
type DeviceResource struct {
	client *balena.Client
}

// DeviceResourceModel describes the device resource data model.
type DeviceResourceModel struct {
	ID            types.Int64  `tfsdk:"id"`
	UUID          types.String `tfsdk:"uuid"`
	DeviceName    types.String `tfsdk:"device_name"`
	PinnedRelease types.String `tfsdk:"pinned_release"`
	OSVersion     types.String `tfsdk:"os_version"`
}

// NewDeviceResource returns a new device resource instance.
func NewDeviceResource() resource.Resource {
	return &DeviceResource{}
}

func (r *DeviceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_device"
}

func (r *DeviceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the name, the release and the host OS of an existing Balena device. A device joins Balena when it " +
			"is provisioned, so creating this resource takes over a device and destroying it leaves the device untouched.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier of the device.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"uuid": schema.StringAttribute{
				Description: "UUID of the device.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"device_name": schema.StringAttribute{
				Description: "Name of the device. When null, the device keeps its current name.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"pinned_release": schema.StringAttribute{
				Description: "Commit of the release that the device is pinned to. The release must be successful and belong to " +
					"the fleet of the device. When null, the device tracks the release of its fleet.",
				Optional: true,
			},
			"os_version": schema.StringAttribute{
				Description: "Version of the host OS release that should operate the device, for example `6.0.10+rev1`. A new " +
					"version starts a host OS update, and the device reboots at its end. The API refuses a downgrade. When null, " +
					"the device keeps its current host OS release.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *DeviceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := configureClient(req.ProviderData, &resp.Diagnostics, "Resource")
	if !ok {
		return
	}
	r.client = client
}

func (r *DeviceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DeviceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.apply(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DeviceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DeviceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	device, err := r.client.GetDeviceByUUID(ctx, state.UUID.ValueString())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading device", err.Error())
		return
	}

	setDeviceState(&state, device)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *DeviceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan DeviceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.apply(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete leaves the device untouched: Terraform stops managing it.
func (r *DeviceResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

func (r *DeviceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("uuid"), req, resp)
}

// apply patches the fields of the device that differ from the plan, then sets the plan to the device that the API
// returns.
func (r *DeviceResource) apply(ctx context.Context, plan *DeviceResourceModel, diags *diag.Diagnostics) {
	device, err := r.client.GetDeviceByUUID(ctx, plan.UUID.ValueString())
	if err != nil {
		diags.AddError("Error reading device", err.Error())
		return
	}

	body := map[string]interface{}{}
	if name := optionalString(plan.DeviceName); name != nil && *name != device.DeviceName {
		body["device_name"] = *name
	}

	commit := optionalString(plan.PinnedRelease)
	currentCommit := device.PinnedReleaseCommit()
	switch {
	case commit == nil && currentCommit != nil:
		body["should_be_running__release"] = nil
	case commit != nil && (currentCommit == nil || *commit != *currentCommit):
		release, err := r.client.GetReleaseByCommit(ctx, device.BelongsToApp.ID, *commit)
		if err != nil {
			diags.AddAttributeError(path.Root("pinned_release"), "Error finding the release to pin", err.Error())
			return
		}
		body["should_be_running__release"] = release.ID
	}

	osVersion := optionalString(plan.OSVersion)
	currentOSVersion := device.HostOSVersion()
	if osVersion != nil && (currentOSVersion == nil || *osVersion != *currentOSVersion) {
		release, err := r.client.GetHostOSRelease(ctx, device.DeviceTypeSlug(), *osVersion)
		if err != nil {
			diags.AddAttributeError(path.Root("os_version"), "Error finding the host OS release", err.Error())
			return
		}
		body["should_be_operated_by__release"] = release.ID
	}

	if len(body) > 0 {
		tflog.Debug(ctx, "Updating device", map[string]interface{}{"uuid": device.UUID, "fields": len(body)})
		if err := r.client.UpdateDevice(ctx, device.ID, body); err != nil {
			diags.AddError("Error updating device", err.Error())
			return
		}
		device, err = r.client.GetDeviceByUUID(ctx, plan.UUID.ValueString())
		if err != nil {
			diags.AddError("Error reading device after update", err.Error())
			return
		}
	}

	setDeviceState(plan, device)
}

// setDeviceState copies the managed fields of the device into the model.
func setDeviceState(model *DeviceResourceModel, device *balena.Device) {
	model.ID = types.Int64Value(device.ID)
	model.DeviceName = types.StringValue(device.DeviceName)
	model.PinnedRelease = types.StringPointerValue(device.PinnedReleaseCommit())
	model.OSVersion = types.StringPointerValue(device.HostOSVersion())
}
