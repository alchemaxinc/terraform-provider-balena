package provider

import (
	"context"
	"fmt"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// childResourceAPI captures the four CRUD operations shared by every
// parent/key/value child resource exposed by the Balena API. The update and
// delete signatures are ordered to match method-value syntax on *balena.Client.
type childResourceAPI struct {
	create func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (id int64, err error)
	read   func(ctx context.Context, c *balena.Client, id int64) (parentID int64, key, value string, err error)
	update func(c *balena.Client, ctx context.Context, id int64, value string) error
	delete func(c *balena.Client, ctx context.Context, id int64) error
}

// childResourceConfig describes a parent/key/value resource declaratively.
type childResourceConfig struct {
	typeSuffix       string
	description      string
	parentAttrName   string
	parentAttrDesc   string
	keyAttrName      string
	keyAttrDesc      string
	keyValidators    []validator.String
	valueSensitive   bool
	valueDescription string
	api              childResourceAPI
}

// childResource is a generic implementation of a parent/key/value resource.
type childResource struct {
	cfg    childResourceConfig
	client *balena.Client
}

// newChildResource returns a resource.Resource implementation for the given config.
func newChildResource(cfg childResourceConfig) resource.Resource {
	return &childResource{cfg: cfg}
}

var (
	_ resource.Resource                = &childResource{}
	_ resource.ResourceWithImportState = &childResource{}
)

func (r *childResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.cfg.typeSuffix
}

func (r *childResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	valueDesc := r.cfg.valueDescription
	if valueDesc == "" {
		valueDesc = "Value."
	}
	resp.Schema = schema.Schema{
		Description: r.cfg.description,
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier assigned by the Balena API.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			r.cfg.parentAttrName: schema.Int64Attribute{
				Description: r.cfg.parentAttrDesc,
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			r.cfg.keyAttrName: schema.StringAttribute{
				Description: r.cfg.keyAttrDesc,
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: r.cfg.keyValidators,
			},
			"value": schema.StringAttribute{
				Description: valueDesc,
				Required:    true,
				Sensitive:   r.cfg.valueSensitive,
			},
		},
	}
}

func (r *childResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := configureClient(req.ProviderData, &resp.Diagnostics, "Resource")
	if !ok {
		return
	}
	r.client = client
}

// readChildAttrs extracts the four attributes from a tfsdk.State or tfsdk.Plan
// using dynamic attribute paths. Returns ok=false if diagnostics have errors.
func (r *childResource) readChildAttrs(ctx context.Context, plan tfsdk.Plan, diags *diag.Diagnostics) (id, parentID types.Int64, key, value types.String, ok bool) {
	diags.Append(plan.GetAttribute(ctx, path.Root("id"), &id)...)
	diags.Append(plan.GetAttribute(ctx, path.Root(r.cfg.parentAttrName), &parentID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root(r.cfg.keyAttrName), &key)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("value"), &value)...)
	ok = !diags.HasError()
	return
}

func (r *childResource) readChildAttrsFromState(ctx context.Context, state tfsdk.State, diags *diag.Diagnostics) (id, parentID types.Int64, key, value types.String, ok bool) {
	diags.Append(state.GetAttribute(ctx, path.Root("id"), &id)...)
	diags.Append(state.GetAttribute(ctx, path.Root(r.cfg.parentAttrName), &parentID)...)
	diags.Append(state.GetAttribute(ctx, path.Root(r.cfg.keyAttrName), &key)...)
	diags.Append(state.GetAttribute(ctx, path.Root("value"), &value)...)
	ok = !diags.HasError()
	return
}

func (r *childResource) writeChildAttrs(ctx context.Context, state *tfsdk.State, diags *diag.Diagnostics, id, parentID types.Int64, key, value types.String) {
	diags.Append(state.SetAttribute(ctx, path.Root("id"), id)...)
	diags.Append(state.SetAttribute(ctx, path.Root(r.cfg.parentAttrName), parentID)...)
	diags.Append(state.SetAttribute(ctx, path.Root(r.cfg.keyAttrName), key)...)
	diags.Append(state.SetAttribute(ctx, path.Root("value"), value)...)
}

func (r *childResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	_, parentID, key, value, ok := r.readChildAttrs(ctx, req.Plan, &resp.Diagnostics)
	if !ok {
		return
	}

	id, err := r.cfg.api.create(ctx, r.client, parentID.ValueInt64(), key.ValueString(), value.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error creating %s", r.cfg.typeSuffix), err.Error())
		return
	}
	r.writeChildAttrs(ctx, &resp.State, &resp.Diagnostics, types.Int64Value(id), parentID, key, value)
}

func (r *childResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	id, _, _, _, ok := r.readChildAttrsFromState(ctx, req.State, &resp.Diagnostics)
	if !ok {
		return
	}

	parentID, key, value, err := r.cfg.api.read(ctx, r.client, id.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(fmt.Sprintf("Error reading %s", r.cfg.typeSuffix), err.Error())
		return
	}

	r.writeChildAttrs(ctx, &resp.State, &resp.Diagnostics,
		id,
		types.Int64Value(parentID),
		types.StringValue(key),
		types.StringValue(value),
	)
}

func (r *childResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	_, parentID, key, value, ok := r.readChildAttrs(ctx, req.Plan, &resp.Diagnostics)
	if !ok {
		return
	}
	var stateID types.Int64
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &stateID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.cfg.api.update(r.client, ctx, stateID.ValueInt64(), value.ValueString()); err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error updating %s", r.cfg.typeSuffix), err.Error())
		return
	}
	r.writeChildAttrs(ctx, &resp.State, &resp.Diagnostics, stateID, parentID, key, value)
}

func (r *childResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var id types.Int64
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.cfg.api.delete(r.client, ctx, id.ValueInt64()); err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError(fmt.Sprintf("Error deleting %s", r.cfg.typeSuffix), err.Error())
	}
}

func (r *childResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := parseID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric ID, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}

// configureClient validates provider data is a *balena.Client. On mismatch it
// appends a diagnostic and returns ok=false. role should be "Resource" or
// "Data Source" for the error message.
func configureClient(providerData interface{}, diags *diag.Diagnostics, role string) (*balena.Client, bool) {
	if providerData == nil {
		return nil, false
	}
	client, ok := providerData.(*balena.Client)
	if !ok {
		diags.AddError(
			fmt.Sprintf("Unexpected %s Configure Type", role),
			fmt.Sprintf("Expected *balena.Client, got %T. This is a bug in the provider — please report it.", providerData),
		)
		return nil, false
	}
	return client, true
}
