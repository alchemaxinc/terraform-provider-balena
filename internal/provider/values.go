package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// attributeGetter is implemented by tfsdk.Plan, tfsdk.State and tfsdk.Config.
type attributeGetter interface {
	GetAttribute(ctx context.Context, p path.Path, target interface{}) diag.Diagnostics
}

// attributeSetter is implemented by *tfsdk.State.
type attributeSetter interface {
	SetAttribute(ctx context.Context, p path.Path, val interface{}) diag.Diagnostics
}

// getValue reads an attribute as a typed framework value.
func getValue(ctx context.Context, g attributeGetter, a *AttributeSpec) (attr.Value, diag.Diagnostics) {
	p := path.Root(a.Name)
	switch a.Type {
	case ATTR_INT64:
		var v types.Int64
		d := g.GetAttribute(ctx, p, &v)
		return v, d
	case ATTR_BOOL:
		var v types.Bool
		d := g.GetAttribute(ctx, p, &v)
		return v, d
	case ATTR_FLOAT64:
		var v types.Float64
		d := g.GetAttribute(ctx, p, &v)
		return v, d
	default:
		var v types.String
		d := g.GetAttribute(ctx, p, &v)
		return v, d
	}
}

// getValues reads the attributes for which include returns true.
func getValues(ctx context.Context, g attributeGetter, spec *TableSpec, include func(*AttributeSpec) bool) (map[string]attr.Value, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := map[string]attr.Value{}
	for _, a := range spec.Attributes {
		if !include(a) {
			continue
		}
		v, d := getValue(ctx, g, a)
		diags.Append(d...)
		out[a.Name] = v
	}
	return out, diags
}

// nullValue returns the typed null of an attribute.
func nullValue(a *AttributeSpec) attr.Value {
	switch a.Type {
	case ATTR_INT64:
		return types.Int64Null()
	case ATTR_BOOL:
		return types.BoolNull()
	case ATTR_FLOAT64:
		return types.Float64Null()
	default:
		return types.StringNull()
	}
}

// scalarToAPI converts a known framework value to its JSON value; null becomes nil.
func scalarToAPI(v attr.Value) any {
	if v.IsNull() {
		return nil
	}
	switch x := v.(type) {
	case types.String:
		return x.ValueString()
	case types.Int64:
		return x.ValueInt64()
	case types.Bool:
		return x.ValueBool()
	case types.Float64:
		return x.ValueFloat64()
	}
	return nil
}

// fromAPI converts the row value of an attribute to a framework value.
func fromAPI(a *AttributeSpec, row balena.Row) (attr.Value, error) {
	raw, present := row[a.Field]
	if !present {
		return nil, fmt.Errorf("the API response has no %s field", a.Field)
	}
	switch a.Encoding {
	case ENC_REF:
		id, ok, err := balena.RefID(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.Field, err)
		}
		if !ok {
			return types.Int64Null(), nil
		}
		return types.Int64Value(id), nil
	case ENC_REF_BY:
		ref, ok, err := balena.Expanded(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.Field, err)
		}
		if !ok {
			return nullValue(a), nil
		}
		return scalarFromAPI(a, ref[a.RefField])
	case ENC_JSON:
		if raw == nil {
			return types.StringNull(), nil
		}
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		return types.StringValue(string(data)), nil
	}
	return scalarFromAPI(a, raw)
}

func scalarFromAPI(a *AttributeSpec, raw any) (attr.Value, error) {
	if raw == nil {
		return nullValue(a), nil
	}
	switch a.Type {
	case ATTR_INT64:
		n, _, err := balena.ToInt64(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.Field, err)
		}
		return types.Int64Value(n), nil
	case ATTR_FLOAT64:
		switch x := raw.(type) {
		case json.Number:
			f, err := x.Float64()
			if err != nil {
				return nil, err
			}
			return types.Float64Value(f), nil
		case float64:
			return types.Float64Value(x), nil
		case string:
			f, err := strconv.ParseFloat(x, 64)
			if err != nil {
				return nil, err
			}
			return types.Float64Value(f), nil
		}
	case ATTR_BOOL:
		if b, ok := raw.(bool); ok {
			return types.BoolValue(b), nil
		}
	default:
		switch x := raw.(type) {
		case string:
			return types.StringValue(x), nil
		case json.Number:
			return types.StringValue(x.String()), nil
		}
	}
	return nil, fmt.Errorf("%s: unexpected value %v", a.Field, raw)
}

// rowValues converts a row to framework values for the attributes for which include returns true.
func rowValues(spec *TableSpec, row balena.Row, include func(*AttributeSpec) bool) (map[string]attr.Value, error) {
	out := map[string]attr.Value{}
	for _, a := range spec.Attributes {
		if !include(a) {
			continue
		}
		v, err := fromAPI(a, row)
		if err != nil {
			return nil, err
		}
		out[a.Name] = v
	}
	return out, nil
}

// setValues writes values to a state.
func setValues(ctx context.Context, s attributeSetter, values map[string]attr.Value) diag.Diagnostics {
	var diags diag.Diagnostics
	for name, v := range values {
		diags.Append(s.SetAttribute(ctx, path.Root(name), v)...)
	}
	return diags
}

// sameValue reports whether two values of an attribute are equal. Date-times are equal when they name the same
// instant: the API answers `2026-12-01T00:00:00.000Z` for `2026-12-01T01:00:00+01:00`.
func sameValue(a *AttributeSpec, x, y attr.Value) bool {
	if x.Equal(y) {
		return true
	}
	if !a.DateTime {
		return false
	}
	xt, xok := instant(x)
	yt, yok := instant(y)
	return xok && yok && xt.Equal(yt)
}

func instant(v attr.Value) (time.Time, bool) {
	s, ok := v.(types.String)
	if !ok || s.IsNull() || s.IsUnknown() {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s.ValueString())
	return t, err == nil
}

// keepNotation keeps the prior notation of date-times the API returned as the same instant, so equal instants never
// show as a change.
func keepNotation(spec *TableSpec, prior, values map[string]attr.Value) {
	for _, a := range spec.Attributes {
		if p, ok := prior[a.Name]; ok && a.DateTime {
			if v, ok := values[a.Name]; ok && !p.Equal(v) && sameValue(a, p, v) {
				values[a.Name] = p
			}
		}
	}
}

// filterValue converts a configured data source attribute to the value its $filter compares with.
func filterValue(v attr.Value) any {
	return scalarToAPI(v)
}
