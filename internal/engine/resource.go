package engine

import (
	"context"
	"fmt"
	"math/big"
	"strconv"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var (
	_ resource.Resource                = &genericResource{}
	_ resource.ResourceWithConfigure   = &genericResource{}
	_ resource.ResourceWithImportState = &genericResource{}
)

// genericResource implements a Terraform resource from a descriptor.
type genericResource struct {
	d      Resource
	client *balena.Client
}

// NewResource returns a resource implementation for the descriptor.
func NewResource(d Resource) resource.Resource {
	return &genericResource{d: d}
}

// ResourceFactories returns a factory for every descriptor.
func ResourceFactories(ds []Resource) []func() resource.Resource {
	out := make([]func() resource.Resource, 0, len(ds))
	for _, d := range ds {
		out = append(out, func() resource.Resource { return NewResource(d) })
	}
	return out
}

func (r *genericResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.d.TypeName
}

func (r *genericResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceSchema(r.d)
}

func (r *genericResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (r *genericResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	plan, err := objectValues(req.Plan.Raw)
	if err != nil {
		resp.Diagnostics.AddError("Error reading plan", err.Error())
		return
	}
	body, err := r.body(plan, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error building request", err.Error())
		return
	}
	created, err := r.client.CreateEntity(ctx, r.d.Path, body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating "+r.d.TypeName, err.Error())
		return
	}
	id, err := created.ID()
	if err != nil {
		resp.Diagnostics.AddError("Error creating "+r.d.TypeName, err.Error())
		return
	}
	v, err := r.fetch(ctx, id, plan, true, resp.State.Schema.Type().TerraformType(ctx))
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+r.d.TypeName+" after create", err.Error())
		return
	}
	resp.State.Raw = v
}

func (r *genericResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	prior, err := objectValues(req.State.Raw)
	if err != nil {
		resp.Diagnostics.AddError("Error reading state", err.Error())
		return
	}
	id, err := int64Value(prior[ID_ATTRIBUTE])
	if err != nil {
		resp.Diagnostics.AddError("Error reading state", err.Error())
		return
	}
	v, err := r.fetch(ctx, id, prior, false, resp.State.Schema.Type().TerraformType(ctx))
	if balena.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+r.d.TypeName, err.Error())
		return
	}
	resp.State.Raw = v
}

func (r *genericResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	plan, err := objectValues(req.Plan.Raw)
	if err != nil {
		resp.Diagnostics.AddError("Error reading plan", err.Error())
		return
	}
	state, err := objectValues(req.State.Raw)
	if err != nil {
		resp.Diagnostics.AddError("Error reading state", err.Error())
		return
	}
	id, err := int64Value(state[ID_ATTRIBUTE])
	if err != nil {
		resp.Diagnostics.AddError("Error reading state", err.Error())
		return
	}
	body, err := r.body(plan, state)
	if err != nil {
		resp.Diagnostics.AddError("Error building request", err.Error())
		return
	}
	if len(body) > 0 {
		if err := r.client.PatchEntity(ctx, r.d.Path, id, body); err != nil {
			resp.Diagnostics.AddError("Error updating "+r.d.TypeName, err.Error())
			return
		}
	}
	v, err := r.fetch(ctx, id, plan, true, resp.State.Schema.Type().TerraformType(ctx))
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+r.d.TypeName+" after update", err.Error())
		return
	}
	resp.State.Raw = v
}

func (r *genericResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	state, err := objectValues(req.State.Raw)
	if err != nil {
		resp.Diagnostics.AddError("Error reading state", err.Error())
		return
	}
	id, err := int64Value(state[ID_ATTRIBUTE])
	if err != nil {
		resp.Diagnostics.AddError("Error reading state", err.Error())
		return
	}
	if err := r.client.DeleteEntity(ctx, r.d.Path, id); err != nil && !balena.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting "+r.d.TypeName, err.Error())
	}
}

func (r *genericResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected a numeric id, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(ID_ATTRIBUTE), types.Int64Value(id))...)
}

// body builds a request body from the planned values. With a prior state it
// holds only the attributes whose planned value changed; a removed optional
// attribute is sent as null to clear it.
func (r *genericResource) body(plan, state map[string]tftypes.Value) (map[string]any, error) {
	body := map[string]any{}
	for _, a := range r.d.Attributes {
		if !a.writable() {
			continue
		}
		v, ok := plan[a.Name]
		if !ok || !v.IsKnown() {
			continue
		}
		if state == nil {
			if v.IsNull() {
				continue
			}
		} else if old, ok := state[a.Name]; ok && v.Equal(old) {
			continue
		}
		w, err := a.toWire(v)
		if err != nil {
			return nil, err
		}
		body[a.Wire] = w
	}
	return body, nil
}

// fetch reads the entity and returns it as a state value. After an apply
// (keepPlanned) every known planned value is kept, as Terraform requires;
// unknown values come from the API. Write-only values always come from prior.
func (r *genericResource) fetch(ctx context.Context, id int64, prior map[string]tftypes.Value, keepPlanned bool, typ tftypes.Type) (tftypes.Value, error) {
	e, err := r.client.GetEntity(ctx, r.d.Path, id)
	if err != nil {
		return tftypes.Value{}, err
	}
	vals, err := entityValues(r.d.Attributes, e, prior, keepPlanned)
	if err != nil {
		return tftypes.Value{}, err
	}
	return tftypes.NewValue(typ, vals), nil
}

// entityValues decodes an entity into attribute values.
func entityValues(attrs []Attribute, e balena.Entity, prior map[string]tftypes.Value, keepPlanned bool) (map[string]tftypes.Value, error) {
	id, err := e.ID()
	if err != nil {
		return nil, err
	}
	vals := map[string]tftypes.Value{ID_ATTRIBUTE: tftypes.NewValue(tftypes.Number, new(big.Float).SetInt64(id))}
	for _, a := range attrs {
		p, hasPrior := prior[a.Name]
		hasPrior = hasPrior && p.IsKnown()
		if a.WriteOnly {
			if hasPrior {
				vals[a.Name] = p
			} else {
				vals[a.Name] = a.null()
			}
			continue
		}
		if keepPlanned && hasPrior {
			vals[a.Name] = p
			continue
		}
		v, err := a.fromWire(e[a.Wire])
		if err != nil {
			return nil, err
		}
		if a.Codec == CODEC_JSON && hasPrior && !p.IsNull() && !v.IsNull() {
			var ps, vs string
			if p.As(&ps) == nil && v.As(&vs) == nil && sameJSON(ps, vs) {
				v = p
			}
		}
		vals[a.Name] = v
	}
	return vals, nil
}

// objectValues splits an object value into its attributes.
func objectValues(v tftypes.Value) (map[string]tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if v.IsNull() {
		return vals, nil
	}
	if err := v.As(&vals); err != nil {
		return nil, err
	}
	return vals, nil
}

// int64Value decodes a known, non-null number.
func int64Value(v tftypes.Value) (int64, error) {
	if !v.IsKnown() || v.IsNull() {
		return 0, fmt.Errorf("id is not set")
	}
	var f big.Float
	if err := v.As(&f); err != nil {
		return 0, err
	}
	n, acc := f.Int64()
	if acc != big.Exact {
		return 0, fmt.Errorf("id %s is not an integer", f.String())
	}
	return n, nil
}

// configureClient extracts the Balena client from provider data.
func configureClient(data any, diags *diag.Diagnostics) *balena.Client {
	if data == nil {
		return nil
	}
	c, ok := data.(*balena.Client)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("Expected *balena.Client, got %T.", data))
		return nil
	}
	return c
}
