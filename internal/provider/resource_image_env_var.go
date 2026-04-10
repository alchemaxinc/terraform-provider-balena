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
	_ resource.Resource                = &ImageEnvVarResource{}
	_ resource.ResourceWithImportState = &ImageEnvVarResource{}
)

// ImageEnvVarResource implements the balena_image_env_var resource.
type ImageEnvVarResource struct {
	client *balena.Client
}

// ImageEnvVarResourceModel describes the image env var data model.
type ImageEnvVarResourceModel struct {
	ID             types.Int64  `tfsdk:"id"`
	ReleaseImageID types.Int64  `tfsdk:"release_image_id"`
	Name           types.String `tfsdk:"name"`
	Value          types.String `tfsdk:"value"`
}

// NewImageEnvVarResource returns a new image env var resource instance.
func NewImageEnvVarResource() resource.Resource {
	return &ImageEnvVarResource{}
}

func (r *ImageEnvVarResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_env_var"
}

func (r *ImageEnvVarResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an image-level environment variable.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"release_image_id": schema.Int64Attribute{
				Description:         "ID of the release image (image__is_part_of__release).",
				MarkdownDescription: "ID of the release image (`image__is_part_of__release`).",
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

func (r *ImageEnvVarResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ImageEnvVarResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ImageEnvVarResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.CreateImageEnvVar(ctx, plan.ReleaseImageID.ValueInt64(), plan.Name.ValueString(), plan.Value.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating image env var", err.Error())
		return
	}

	plan.ID = types.Int64Value(result.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ImageEnvVarResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ImageEnvVarResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.GetImageEnvVar(ctx, state.ID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading image env var", err.Error())
		return
	}

	state.ReleaseImageID = types.Int64Value(result.ReleaseImage.ID)
	state.Name = types.StringValue(result.Name)
	state.Value = types.StringValue(result.Value)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ImageEnvVarResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ImageEnvVarResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state ImageEnvVarResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.UpdateImageEnvVar(ctx, state.ID.ValueInt64(), plan.Value.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error updating image env var", err.Error())
		return
	}

	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ImageEnvVarResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ImageEnvVarResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteImageEnvVar(ctx, state.ID.ValueInt64())
	if err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting image env var", err.Error())
	}
}

func (r *ImageEnvVarResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric ID, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}
