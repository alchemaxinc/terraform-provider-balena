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
	_ resource.Resource                = &ApplicationConfigVarResource{}
	_ resource.ResourceWithImportState = &ApplicationConfigVarResource{}
)

// ApplicationConfigVarResource implements the balena_application_config_var resource.
type ApplicationConfigVarResource struct {
	client *balena.Client
}

// ApplicationConfigVarResourceModel describes the application config var data model.
type ApplicationConfigVarResourceModel struct {
	ID            types.Int64  `tfsdk:"id"`
	ApplicationID types.Int64  `tfsdk:"application_id"`
	Name          types.String `tfsdk:"name"`
	Value         types.String `tfsdk:"value"`
}

// NewApplicationConfigVarResource returns a new application config var resource instance.
func NewApplicationConfigVarResource() resource.Resource {
	return &ApplicationConfigVarResource{}
}

func (r *ApplicationConfigVarResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_config_var"
}

func (r *ApplicationConfigVarResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an application-level configuration variable.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"application_id": schema.Int64Attribute{
				Description: "ID of the application.",
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

func (r *ApplicationConfigVarResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ApplicationConfigVarResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ApplicationConfigVarResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.CreateApplicationConfigVar(ctx, plan.ApplicationID.ValueInt64(), plan.Name.ValueString(), plan.Value.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating application config var", err.Error())
		return
	}

	plan.ID = types.Int64Value(result.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ApplicationConfigVarResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ApplicationConfigVarResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.GetApplicationConfigVar(ctx, state.ID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading application config var", err.Error())
		return
	}

	state.ApplicationID = types.Int64Value(result.App.ID)
	state.Name = types.StringValue(result.Name)
	state.Value = types.StringValue(result.Value)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ApplicationConfigVarResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ApplicationConfigVarResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state ApplicationConfigVarResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.UpdateApplicationConfigVar(ctx, state.ID.ValueInt64(), plan.Value.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error updating application config var", err.Error())
		return
	}

	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ApplicationConfigVarResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ApplicationConfigVarResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteApplicationConfigVar(ctx, state.ID.ValueInt64())
	if err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting application config var", err.Error())
	}
}

func (r *ApplicationConfigVarResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric ID, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}
