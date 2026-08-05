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
	_ resource.Resource                = &ImageProfileResource{}
	_ resource.ResourceWithImportState = &ImageProfileResource{}
)

// ImageProfileResource implements the balena_image_profile resource.
type ImageProfileResource struct {
	client *balena.Client
}

// ImageProfileResourceModel describes the image_profile resource data model.
type ImageProfileResourceModel struct {
	ID             types.Int64  `tfsdk:"id"`
	ReleaseImageID types.Int64  `tfsdk:"release_image_id"`
	ProfileName    types.String `tfsdk:"profile_name"`
}

// NewImageProfileResource returns a new image_profile resource instance.
func NewImageProfileResource() resource.Resource {
	return &ImageProfileResource{}
}

func (r *ImageProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_profile"
}

func (r *ImageProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a profile name on a Balena release image.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier of the image profile.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"release_image_id": schema.Int64Attribute{
				Description: "Numeric ID of the parent release image.",
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

func (r *ImageProfileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := configureClient(req.ProviderData, &resp.Diagnostics, "Resource")
	if !ok {
		return
	}
	r.client = client
}

func (r *ImageProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ImageProfileResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "creating image profile", map[string]any{
		"release_image_id": plan.ReleaseImageID.ValueInt64(),
		"profile_name":     plan.ProfileName.ValueString(),
	})

	ip, err := r.client.CreateImageProfile(ctx, plan.ReleaseImageID.ValueInt64(), plan.ProfileName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating image_profile", err.Error())
		return
	}

	plan.ID = types.Int64Value(ip.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ImageProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ImageProfileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "reading image profile", map[string]any{"id": state.ID.ValueInt64()})

	ip, err := r.client.GetImageProfile(ctx, state.ID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading image_profile", err.Error())
		return
	}

	state.ReleaseImageID = types.Int64Value(ip.ReleaseImage.ID)
	state.ProfileName = types.StringValue(ip.ProfileName)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ImageProfileResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
	// All attributes require replace; Update is never called.
}

func (r *ImageProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ImageProfileResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "deleting image profile", map[string]any{"id": state.ID.ValueInt64()})

	if err := r.client.DeleteImageProfile(ctx, state.ID.ValueInt64()); err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting image_profile", err.Error())
	}
}

func (r *ImageProfileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric ID, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}
