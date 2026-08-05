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
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &ApplicationProfileResource{}
	_ resource.ResourceWithImportState = &ApplicationProfileResource{}
)

// ApplicationProfileResource implements the balena_application_profile resource.
type ApplicationProfileResource struct {
	client *balena.Client
}

// ApplicationProfileResourceModel describes the application_profile resource data model.
type ApplicationProfileResourceModel struct {
	ID            types.Int64  `tfsdk:"id"`
	ApplicationID types.Int64  `tfsdk:"application_id"`
	HostAppID     types.Int64  `tfsdk:"host_app_id"`
	ProfileName   types.String `tfsdk:"profile_name"`
}

// NewApplicationProfileResource returns a new application_profile resource instance.
func NewApplicationProfileResource() resource.Resource {
	return &ApplicationProfileResource{}
}

func (r *ApplicationProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_profile"
}

func (r *ApplicationProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Balena application profile, linking a fleet application to a host application via a profile name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier of the application profile.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"application_id": schema.Int64Attribute{
				Description: "Numeric ID of the fleet application.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"host_app_id": schema.Int64Attribute{
				Description: "Numeric ID of the host application.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"profile_name": schema.StringAttribute{
				Description: "Profile name (1–100 characters).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *ApplicationProfileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := configureClient(req.ProviderData, &resp.Diagnostics, "Resource")
	if !ok {
		return
	}
	r.client = client
}

func (r *ApplicationProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ApplicationProfileResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "creating application profile", map[string]any{
		"application_id": plan.ApplicationID.ValueInt64(),
		"host_app_id":    plan.HostAppID.ValueInt64(),
		"profile_name":   plan.ProfileName.ValueString(),
	})

	ap, err := r.client.CreateApplicationProfile(ctx, plan.ApplicationID.ValueInt64(), plan.HostAppID.ValueInt64(), plan.ProfileName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating application_profile", err.Error())
		return
	}

	plan.ID = types.Int64Value(ap.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ApplicationProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ApplicationProfileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "reading application profile", map[string]any{"id": state.ID.ValueInt64()})

	ap, err := r.client.GetApplicationProfile(ctx, state.ID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading application_profile", err.Error())
		return
	}

	state.ApplicationID = types.Int64Value(ap.Application1.ID)
	state.HostAppID = types.Int64Value(ap.Application2.ID)
	state.ProfileName = types.StringValue(ap.ProfileName)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ApplicationProfileResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
	// All attributes require replace; Update is never called.
}

func (r *ApplicationProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ApplicationProfileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "deleting application profile", map[string]any{"id": state.ID.ValueInt64()})

	if err := r.client.DeleteApplicationProfile(ctx, state.ID.ValueInt64()); err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting application_profile", err.Error())
	}
}

func (r *ApplicationProfileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric ID, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}
