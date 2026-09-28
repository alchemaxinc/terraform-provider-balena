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
	_ resource.Resource                = &ApplicationProfileCatalogResource{}
	_ resource.ResourceWithImportState = &ApplicationProfileCatalogResource{}
)

// ApplicationProfileCatalogResource implements the balena_application_profile_catalog resource.
type ApplicationProfileCatalogResource struct {
	client *balena.Client
}

// ApplicationProfileCatalogResourceModel describes the application profile catalog data model.
type ApplicationProfileCatalogResourceModel struct {
	ID            types.Int64  `tfsdk:"id"`
	ApplicationID types.Int64  `tfsdk:"application_id"`
	ProfileName   types.String `tfsdk:"profile_name"`
	Description   types.String `tfsdk:"description"`
}

// NewApplicationProfileCatalogResource returns a new application profile catalog resource instance.
func NewApplicationProfileCatalogResource() resource.Resource {
	return &ApplicationProfileCatalogResource{}
}

func (r *ApplicationProfileCatalogResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_profile_catalog"
}

func (r *ApplicationProfileCatalogResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Catalogs a profile name offered by an application.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier assigned by the Balena API.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"application_id": schema.Int64Attribute{
				Description: "Numeric ID of the application cataloguing the profile.",
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
			"description": schema.StringAttribute{
				Description: "Human-readable description of the profile, at most 4000 characters.",
				Optional:    true,
				Validators:  []validator.String{profileDescriptionValidator},
			},
		},
	}
}

func (r *ApplicationProfileCatalogResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := configureClient(req.ProviderData, &resp.Diagnostics, "Resource")
	if !ok {
		return
	}
	r.client = client
}

func (r *ApplicationProfileCatalogResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ApplicationProfileCatalogResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.CreateApplicationProfileCatalog(ctx, plan.ApplicationID.ValueInt64(), plan.ProfileName.ValueString(), optionalString(plan.Description))
	if err != nil {
		resp.Diagnostics.AddError("Error creating application profile catalog", err.Error())
		return
	}

	plan.ID = types.Int64Value(result.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ApplicationProfileCatalogResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ApplicationProfileCatalogResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.GetApplicationProfileCatalog(ctx, state.ID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading application profile catalog", err.Error())
		return
	}

	state.ApplicationID = types.Int64Value(result.App.ID)
	state.ProfileName = types.StringValue(result.ProfileName)
	if result.Description == nil {
		state.Description = types.StringNull()
	} else {
		state.Description = types.StringValue(*result.Description)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ApplicationProfileCatalogResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ApplicationProfileCatalogResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID
	if !plan.Description.Equal(state.Description) {
		if err := r.client.UpdateApplicationProfileCatalog(ctx, plan.ID.ValueInt64(), optionalString(plan.Description)); err != nil {
			resp.Diagnostics.AddError("Error updating application profile catalog", err.Error())
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ApplicationProfileCatalogResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ApplicationProfileCatalogResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteApplicationProfileCatalog(ctx, state.ID.ValueInt64()); err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting application profile catalog", err.Error())
	}
}

func (r *ApplicationProfileCatalogResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric ID, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}
