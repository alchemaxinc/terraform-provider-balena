package provider

import (
	"context"
	"fmt"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &ApplicationResource{}
	_ resource.ResourceWithImportState = &ApplicationResource{}
)

// ApplicationResource implements the balena_application resource.
type ApplicationResource struct {
	client *balena.Client
}

// ApplicationResourceModel describes the application resource data model.
type ApplicationResourceModel struct {
	ID             types.Int64  `tfsdk:"id"`
	AppName        types.String `tfsdk:"app_name"`
	Slug           types.String `tfsdk:"slug"`
	DeviceType     types.String `tfsdk:"device_type"`
	OrganizationID types.Int64  `tfsdk:"organization_id"`
	IsPublic       types.Bool   `tfsdk:"is_public"`
	IsArchived     types.Bool   `tfsdk:"is_archived"`
}

// NewApplicationResource returns a new application resource instance.
func NewApplicationResource() resource.Resource {
	return &ApplicationResource{}
}

func (r *ApplicationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application"
}

func (r *ApplicationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Balena application (fleet).",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier of the application.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"app_name": schema.StringAttribute{
				Description: "Name of the application.",
				Required:    true,
			},
			"slug": schema.StringAttribute{
				Description: "Slug of the application (org/name).",
				Computed:    true,
			},
			"device_type": schema.StringAttribute{
				Description: "Device type for the application (e.g. raspberrypi4-64).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"organization_id": schema.Int64Attribute{
				Description: "ID of the organization that owns this application.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"is_public": schema.BoolAttribute{
				Description: "Whether the application is publicly visible. Can be toggled. Defaults to the value returned by the API on creation.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"is_archived": schema.BoolAttribute{
				Description: "Whether the application is archived. This attribute is read-only; archive/unarchive an application via the Balena dashboard or API.",
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ApplicationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := configureClient(req.ProviderData, &resp.Diagnostics, "Resource")
	if !ok {
		return
	}
	r.client = client
}

func (r *ApplicationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ApplicationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var isPublic *bool
	if !plan.IsPublic.IsNull() && !plan.IsPublic.IsUnknown() {
		v := plan.IsPublic.ValueBool()
		isPublic = &v
	}

	tflog.Debug(ctx, "Creating application", map[string]interface{}{"app_name": plan.AppName.ValueString()})

	app, err := r.client.CreateApplication(ctx, plan.AppName.ValueString(), plan.DeviceType.ValueString(), plan.OrganizationID.ValueInt64(), isPublic)
	if err != nil {
		resp.Diagnostics.AddError("Error creating application", err.Error())
		return
	}

	plan.ID = types.Int64Value(app.ID)
	plan.Slug = types.StringValue(app.Slug)
	plan.IsPublic = types.BoolValue(app.IsPublic)
	plan.IsArchived = types.BoolValue(app.IsArchived)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ApplicationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ApplicationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	app, err := r.client.GetApplication(ctx, state.ID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading application", err.Error())
		return
	}

	state.AppName = types.StringValue(app.AppName)
	state.Slug = types.StringValue(app.Slug)
	state.DeviceType = types.StringValue(app.DeviceTypeSlug())
	state.OrganizationID = types.Int64Value(app.Org.ID)
	state.IsPublic = types.BoolValue(app.IsPublic)
	state.IsArchived = types.BoolValue(app.IsArchived)

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *ApplicationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ApplicationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state ApplicationResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{}
	if plan.AppName.ValueString() != state.AppName.ValueString() {
		body["app_name"] = plan.AppName.ValueString()
	}
	if !plan.IsPublic.IsNull() && !plan.IsPublic.IsUnknown() &&
		plan.IsPublic.ValueBool() != state.IsPublic.ValueBool() {
		body["is_public"] = plan.IsPublic.ValueBool()
	}

	if len(body) > 0 {
		err := r.client.UpdateApplication(ctx, state.ID.ValueInt64(), body)
		if err != nil {
			resp.Diagnostics.AddError("Error updating application", err.Error())
			return
		}
	}

	app, err := r.client.GetApplication(ctx, state.ID.ValueInt64())
	if err != nil {
		resp.Diagnostics.AddError("Error reading application after update", err.Error())
		return
	}

	plan.ID = state.ID
	plan.Slug = types.StringValue(app.Slug)
	plan.IsPublic = types.BoolValue(app.IsPublic)
	plan.IsArchived = types.BoolValue(app.IsArchived)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ApplicationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ApplicationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteApplication(ctx, state.ID.ValueInt64())
	if err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting application", err.Error())
	}
}

func (r *ApplicationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric ID, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}
