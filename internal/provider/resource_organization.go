package provider

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &OrganizationResource{}
	_ resource.ResourceWithImportState = &OrganizationResource{}
)

// OrganizationResource implements the balena_organization resource.
type OrganizationResource struct {
	client *balena.Client
}

// OrganizationResourceModel describes the organization resource data model.
type OrganizationResourceModel struct {
	ID     types.Int64  `tfsdk:"id"`
	Name   types.String `tfsdk:"name"`
	Handle types.String `tfsdk:"handle"`
}

// NewOrganizationResource returns a new organization resource instance.
func NewOrganizationResource() resource.Resource {
	return &OrganizationResource{}
}

func (r *OrganizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *OrganizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Balena organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier of the organization.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Display name of the organization.",
				Required:    true,
			},
			"handle": schema.StringAttribute{
				Description: "URL-safe handle of the organization. Only alphanumeric characters and underscores are allowed. Auto-generated from name if not set.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^[a-zA-Z0-9_]+$`),
						"handles can only contain alphanumeric characters and underscores",
					),
				},
			},
		},
	}
}

func (r *OrganizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *OrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	handle := ""
	if !plan.Handle.IsNull() && !plan.Handle.IsUnknown() {
		handle = plan.Handle.ValueString()
	}

	tflog.Debug(ctx, "Creating organization", map[string]interface{}{"name": plan.Name.ValueString()})

	org, err := r.client.CreateOrganization(ctx, plan.Name.ValueString(), handle)
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization", err.Error())
		return
	}

	plan.ID = types.Int64Value(org.ID)
	plan.Name = types.StringValue(org.Name)
	plan.Handle = types.StringValue(org.Handle)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	org, err := r.client.GetOrganization(ctx, state.ID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading organization", err.Error())
		return
	}

	state.Name = types.StringValue(org.Name)
	state.Handle = types.StringValue(org.Handle)

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan OrganizationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state OrganizationResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{}
	if plan.Name.ValueString() != state.Name.ValueString() {
		body["name"] = plan.Name.ValueString()
	}
	if !plan.Handle.IsNull() && !plan.Handle.IsUnknown() && plan.Handle.ValueString() != state.Handle.ValueString() {
		body["handle"] = plan.Handle.ValueString()
	}

	if len(body) > 0 {
		err := r.client.UpdateOrganization(ctx, state.ID.ValueInt64(), body)
		if err != nil {
			resp.Diagnostics.AddError("Error updating organization", err.Error())
			return
		}
	}

	org, err := r.client.GetOrganization(ctx, state.ID.ValueInt64())
	if err != nil {
		resp.Diagnostics.AddError("Error reading organization after update", err.Error())
		return
	}

	plan.ID = state.ID
	plan.Name = types.StringValue(org.Name)
	plan.Handle = types.StringValue(org.Handle)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OrganizationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteOrganization(ctx, state.ID.ValueInt64())
	if err != nil && !balena.IsNotFound(err) {
		// The Balena API does not support organization deletion via API tokens.
		// Treat 401 as a successful removal from state so Terraform can proceed.
		var apiErr *balena.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 401 {
			tflog.Warn(ctx, "Organization deletion is not supported by the Balena API; removing from state only")
			return
		}
		resp.Diagnostics.AddError("Error deleting organization", err.Error())
	}
}

func (r *OrganizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric ID, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}
