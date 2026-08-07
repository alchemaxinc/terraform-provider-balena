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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &ApplicationProfileResource{}
	_ resource.ResourceWithImportState = &ApplicationProfileResource{}
)

// ApplicationProfileResource implements the balena_application_profile resource.
type ApplicationProfileResource struct {
	client *balena.Client
}

// ApplicationProfileResourceModel describes the application profile data model.
type ApplicationProfileResourceModel struct {
	ID                types.Int64  `tfsdk:"id"`
	ApplicationID     types.Int64  `tfsdk:"application_id"`
	ProfileName       types.String `tfsdk:"profile_name"`
	HostApplicationID types.Int64  `tfsdk:"host_application_id"`
}

// NewApplicationProfileResource returns a new application profile resource instance.
func NewApplicationProfileResource() resource.Resource {
	return &ApplicationProfileResource{}
}

func (r *ApplicationProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_profile"
}

func (r *ApplicationProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Activates a profile name of a host application on a fleet application.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier assigned by the Balena API.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"application_id": schema.Int64Attribute{
				Description: "Numeric ID of the application activating the profile. Must be of class \"fleet\".",
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
				Description: "Numeric ID of the application providing the profile. Must be a host application whose class is not \"block\".",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
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

	result, err := r.client.CreateApplicationProfile(ctx, plan.ApplicationID.ValueInt64(), plan.ProfileName.ValueString(), plan.HostApplicationID.ValueInt64())
	if err != nil {
		resp.Diagnostics.AddError("Error creating application profile", err.Error())
		return
	}

	plan.ID = types.Int64Value(result.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ApplicationProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ApplicationProfileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.GetApplicationProfile(ctx, state.ID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading application profile", err.Error())
		return
	}

	state.ApplicationID = types.Int64Value(result.Application.ID)
	state.ProfileName = types.StringValue(result.ProfileName)
	state.HostApplicationID = types.Int64Value(result.HostApplication.ID)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is a no-op: every user-settable attribute uses RequiresReplace, so
// Terraform only calls Update when nothing meaningful has changed.
func (r *ApplicationProfileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ApplicationProfileResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ApplicationProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ApplicationProfileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteApplicationProfile(ctx, state.ID.ValueInt64()); err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting application profile", err.Error())
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
