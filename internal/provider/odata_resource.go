package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &odataResource{}
	_ resource.ResourceWithImportState = &odataResource{}
)

// odataResource is the one resource implementation: a TableSpec drives its schema and its CRUD against the API.
type odataResource struct {
	spec   *TableSpec
	client *balena.Client
}

// newODataResource returns the resource of a spec.
func newODataResource(spec *TableSpec) resource.Resource {
	return &odataResource{spec: spec}
}

func inResource(a *AttributeSpec) bool { return a.Resource != MODE_NONE }

func (r *odataResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.spec.TypeSuffix
}

func (r *odataResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{}
	for _, a := range r.spec.Attributes {
		if inResource(a) {
			attrs[a.Name] = resourceAttribute(a)
		}
	}
	resp.Schema = schema.Schema{
		Description:         r.spec.ResourceDescription,
		MarkdownDescription: r.spec.ResourceDescription,
		Attributes:          attrs,
	}
}

func resourceAttribute(a *AttributeSpec) schema.Attribute {
	required := a.Resource == MODE_REQUIRED
	optional := a.Resource == MODE_OPTIONAL || a.Resource == MODE_OPTIONAL_COMPUTED
	computed := a.Resource == MODE_COMPUTED || a.Resource == MODE_OPTIONAL_COMPUTED
	keep := a.Stable && computed
	switch a.Type {
	case ATTR_INT64:
		var mods []planmodifier.Int64
		if a.ForceNew {
			mods = append(mods, int64planmodifier.RequiresReplace())
		}
		if keep {
			mods = append(mods, int64planmodifier.UseStateForUnknown())
		}
		return schema.Int64Attribute{Description: a.Description, MarkdownDescription: a.Description, Required: required,
			Optional: optional, Computed: computed, Sensitive: a.Sensitive, PlanModifiers: mods}
	case ATTR_BOOL:
		var mods []planmodifier.Bool
		if a.ForceNew {
			mods = append(mods, boolplanmodifier.RequiresReplace())
		}
		if keep {
			mods = append(mods, boolplanmodifier.UseStateForUnknown())
		}
		return schema.BoolAttribute{Description: a.Description, MarkdownDescription: a.Description, Required: required,
			Optional: optional, Computed: computed, Sensitive: a.Sensitive, PlanModifiers: mods}
	case ATTR_FLOAT64:
		var mods []planmodifier.Float64
		if a.ForceNew {
			mods = append(mods, float64planmodifier.RequiresReplace())
		}
		if keep {
			mods = append(mods, float64planmodifier.UseStateForUnknown())
		}
		return schema.Float64Attribute{Description: a.Description, MarkdownDescription: a.Description, Required: required,
			Optional: optional, Computed: computed, Sensitive: a.Sensitive, PlanModifiers: mods}
	default:
		var mods []planmodifier.String
		if a.ForceNew {
			mods = append(mods, stringplanmodifier.RequiresReplace())
		}
		if keep {
			mods = append(mods, stringplanmodifier.UseStateForUnknown())
		}
		return schema.StringAttribute{Description: a.Description, MarkdownDescription: a.Description, Required: required,
			Optional: optional, Computed: computed, Sensitive: a.Sensitive, PlanModifiers: mods, Validators: stringValidators(a)}
	}
}

func (r *odataResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := configureClient(req.ProviderData, &resp.Diagnostics, "Resource")
	if !ok {
		return
	}
	r.client = client
}

func (r *odataResource) hookContext() *HookContext {
	return &HookContext{Client: r.client, Spec: r.spec}
}

func (r *odataResource) read(ctx context.Context, id int64) (balena.Row, error) {
	return r.client.Get(ctx, r.spec.Root, r.spec.Table, id, r.spec.query(inResource))
}

// resolve converts a planned value to the value the API expects, resolving references given by a column of the
// referenced row to its id.
func (r *odataResource) resolve(ctx context.Context, id int64, a *AttributeSpec, v attr.Value) (any, error) {
	if v.IsNull() {
		return nil, nil
	}
	if a.Encoding != ENC_REF_BY {
		return scalarToAPI(v), nil
	}
	value := fmt.Sprint(scalarToAPI(v))
	if a.Resolver != nil {
		return a.Resolver(ctx, r.hookContext(), id, value)
	}
	return lookupID(ctx, r.client, r.spec.Root, a.RefTable, balena.Eq(a.RefField, scalarToAPI(v)), a.RefTable+" with "+a.RefField+" "+strconv.Quote(value))
}

// lookupID returns the id of the one row of a table that matches a filter.
func lookupID(ctx context.Context, c *balena.Client, root, table, filter, what string) (int64, error) {
	rows, err := c.List(ctx, root, table, balena.Query{Select: []string{"id"}, Filter: filter, Top: 2})
	if err != nil {
		return 0, err
	}
	switch len(rows) {
	case 0:
		return 0, &balena.APIError{StatusCode: 404, Body: what + " not found"}
	case 1:
		id, ok, err := balena.ToInt64(rows[0]["id"])
		if err != nil || !ok {
			return 0, fmt.Errorf("%s has no id", what)
		}
		return id, nil
	}
	return 0, fmt.Errorf("more than one %s", what)
}

// body builds a write body from planned values. With current set, only values that differ from it are included
// (PATCH); otherwise every configured value is (POST). Unknown values, nulls of attributes the API sets when they are
// unset, and attributes whose change replaces the resource are left out of a PATCH.
func (r *odataResource) body(ctx context.Context, id int64, planned, current map[string]attr.Value, diags *diag.Diagnostics) balena.Row {
	body := balena.Row{}
	for _, a := range r.spec.Attributes {
		if !a.writable() {
			continue
		}
		pv, ok := planned[a.Name]
		if !ok || pv.IsUnknown() {
			continue
		}
		if pv.IsNull() && (current == nil || a.Resource == MODE_OPTIONAL_COMPUTED) {
			continue
		}
		if current != nil {
			if cv, ok := current[a.Name]; ok && pv.Equal(cv) {
				continue
			}
		}
		v, err := r.resolve(ctx, id, a, pv)
		if err != nil {
			diags.AddAttributeError(path.Root(a.Name), "Cannot resolve "+a.Name, err.Error())
			continue
		}
		body[a.Field] = v
	}
	return body
}

// stateFrom merges planned values with the row the API returned: configured values stay as planned, values the plan
// leaves to the API come from the row.
func (r *odataResource) stateFrom(planned map[string]attr.Value, row balena.Row) (map[string]attr.Value, error) {
	values, err := rowValues(r.spec, row, inResource)
	if err != nil {
		return nil, err
	}
	for _, a := range r.spec.Attributes {
		if !a.writable() {
			continue
		}
		if pv, ok := planned[a.Name]; ok && !pv.IsUnknown() {
			values[a.Name] = pv
		}
	}
	return values, nil
}

func (r *odataResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	planned, diags := getValues(ctx, req.Plan, r.spec, inResource)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	var id int64
	if r.spec.Hooks.Create != nil {
		var err error
		if id, err = r.spec.Hooks.Create(ctx, r.hookContext(), planned); err != nil {
			resp.Diagnostics.AddError("Error creating "+r.spec.TypeSuffix, err.Error())
			return
		}
		row, err := r.read(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Error reading "+r.spec.TypeSuffix, err.Error())
			return
		}
		current, err := rowValues(r.spec, row, inResource)
		if err != nil {
			resp.Diagnostics.AddError("Error reading "+r.spec.TypeSuffix, err.Error())
			return
		}
		body := r.body(ctx, id, planned, current, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		if len(body) > 0 {
			tflog.Debug(ctx, "patching adopted row", map[string]any{"table": r.spec.Table, "id": id, "fields": len(body)})
			if err := r.client.Patch(ctx, r.spec.Root, r.spec.Table, id, body); err != nil {
				resp.Diagnostics.AddError("Error updating "+r.spec.TypeSuffix, err.Error())
				return
			}
		}
	} else {
		body := r.body(ctx, 0, planned, nil, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		tflog.Debug(ctx, "creating row", map[string]any{"table": r.spec.Table, "fields": len(body)})
		created, err := r.client.Create(ctx, r.spec.Root, r.spec.Table, body)
		if err != nil {
			resp.Diagnostics.AddError("Error creating "+r.spec.TypeSuffix, err.Error())
			return
		}
		if id, err = balena.CreatedID(created); err != nil {
			resp.Diagnostics.AddError("Error creating "+r.spec.TypeSuffix, err.Error())
			return
		}
	}
	r.finish(ctx, id, planned, &resp.State, &resp.Diagnostics)
}

// finish reads the row back and writes the state.
func (r *odataResource) finish(ctx context.Context, id int64, planned map[string]attr.Value, state attributeSetter, diags *diag.Diagnostics) {
	row, err := r.read(ctx, id)
	if err != nil {
		diags.AddError("Error reading "+r.spec.TypeSuffix, err.Error())
		return
	}
	values, err := r.stateFrom(planned, row)
	if err != nil {
		diags.AddError("Error reading "+r.spec.TypeSuffix, err.Error())
		return
	}
	diags.Append(setValues(ctx, state, values)...)
}

func (r *odataResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var id types.Int64
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rowID := id.ValueInt64()
	if id.IsNull() && r.spec.ImportBy != "" {
		// Imported by a unique column: find the id first.
		a := r.spec.attr(r.spec.ImportBy)
		key, diags := getValue(ctx, req.State, a)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		var err error
		rowID, err = lookupID(ctx, r.client, r.spec.Root, r.spec.Table, balena.Eq(a.Field, scalarToAPI(key)), r.spec.Table+" with "+a.Field+" "+key.String())
		if err != nil {
			resp.Diagnostics.AddError("Error reading "+r.spec.TypeSuffix, err.Error())
			return
		}
	}
	row, err := r.read(ctx, rowID)
	if err != nil {
		if balena.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading "+r.spec.TypeSuffix, err.Error())
		return
	}
	values, err := rowValues(r.spec, row, inResource)
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+r.spec.TypeSuffix, err.Error())
		return
	}
	resp.Diagnostics.Append(setValues(ctx, &resp.State, values)...)
}

func (r *odataResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	planned, diags := getValues(ctx, req.Plan, r.spec, inResource)
	resp.Diagnostics.Append(diags...)
	prior, diags := getValues(ctx, req.State, r.spec, inResource)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := prior["id"].(types.Int64).ValueInt64()
	planned["id"] = prior["id"]
	body := r.body(ctx, id, planned, prior, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if len(body) > 0 {
		tflog.Debug(ctx, "patching row", map[string]any{"table": r.spec.Table, "id": id, "fields": len(body)})
		if err := r.client.Patch(ctx, r.spec.Root, r.spec.Table, id, body); err != nil {
			resp.Diagnostics.AddError("Error updating "+r.spec.TypeSuffix, err.Error())
			return
		}
	}
	r.finish(ctx, id, planned, &resp.State, &resp.Diagnostics)
}

func (r *odataResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var id types.Int64
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var err error
	if r.spec.Hooks.Delete != nil {
		err = r.spec.Hooks.Delete(ctx, r.hookContext(), id.ValueInt64())
	} else {
		err = r.client.Delete(ctx, r.spec.Root, r.spec.Table, id.ValueInt64())
	}
	if err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting "+r.spec.TypeSuffix, err.Error())
	}
}

func (r *odataResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if r.spec.ImportBy != "" {
		resource.ImportStatePassthroughID(ctx, path.Root(r.spec.ImportBy), req, resp)
		return
	}
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric ID, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}
