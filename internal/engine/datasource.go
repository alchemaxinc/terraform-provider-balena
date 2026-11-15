package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var (
	_ datasource.DataSource              = &genericDataSource{}
	_ datasource.DataSourceWithConfigure = &genericDataSource{}
)

// LOOKUP_LIMIT is how many rows a data source lookup requests: two are enough
// to tell a unique match from an ambiguous one.
const LOOKUP_LIMIT = 2

// genericDataSource looks up exactly one entity by id or by equality filters.
type genericDataSource struct {
	d      Resource
	client *balena.Client
}

// NewDataSource returns a data source implementation for the descriptor.
func NewDataSource(d Resource) datasource.DataSource {
	return &genericDataSource{d: d}
}

// DataSourceFactories returns a factory for every descriptor.
func DataSourceFactories(ds []Resource) []func() datasource.DataSource {
	out := make([]func() datasource.DataSource, 0, len(ds))
	for _, d := range ds {
		out = append(out, func() datasource.DataSource { return NewDataSource(d) })
	}
	return out
}

func (s *genericDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + s.d.TypeName
}

func (s *genericDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dataSourceSchema(s.d)
}

func (s *genericDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	s.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (s *genericDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	cfg, err := objectValues(req.Config.Raw)
	if err != nil {
		resp.Diagnostics.AddError("Error reading configuration", err.Error())
		return
	}
	e, err := s.lookup(ctx, cfg)
	if err != nil {
		resp.Diagnostics.AddError("Error looking up "+s.d.TypeName, err.Error())
		return
	}
	vals, err := entityValues(dataSourceAttributes(s.d), e, nil, false)
	if err != nil {
		resp.Diagnostics.AddError("Error decoding "+s.d.TypeName, err.Error())
		return
	}
	resp.State.Raw = tftypes.NewValue(resp.State.Schema.Type().TerraformType(ctx), vals)
}

// lookup fetches by id when it is set, else by the conjunction of all set
// filter arguments, and requires exactly one match.
func (s *genericDataSource) lookup(ctx context.Context, cfg map[string]tftypes.Value) (balena.Entity, error) {
	if v, ok := cfg[ID_ATTRIBUTE]; ok && v.IsKnown() && !v.IsNull() {
		id, err := int64Value(v)
		if err != nil {
			return nil, err
		}
		return s.client.GetEntity(ctx, s.d.Path, id)
	}
	filter, err := s.filter(cfg)
	if err != nil {
		return nil, err
	}
	if filter == "" {
		return nil, fmt.Errorf("set id or at least one other argument to look up a %s", s.d.TypeName)
	}
	found, err := s.client.FindEntities(ctx, s.d.Path, filter, LOOKUP_LIMIT)
	if err != nil {
		return nil, err
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("no %s matches %s", s.d.TypeName, filter)
	case 1:
		return found[0], nil
	}
	return nil, fmt.Errorf("more than one %s matches %s: add arguments to narrow the lookup", s.d.TypeName, filter)
}

// filter builds an OData conjunction of equality tests on the set arguments.
func (s *genericDataSource) filter(cfg map[string]tftypes.Value) (string, error) {
	var parts []string
	for _, a := range s.d.Attributes {
		if !a.Filterable {
			continue
		}
		v, ok := cfg[a.Name]
		if !ok || !v.IsKnown() || v.IsNull() {
			continue
		}
		lit, err := a.filterLiteral(v)
		if err != nil {
			return "", err
		}
		parts = append(parts, a.Wire+" eq "+lit)
	}
	return strings.Join(parts, " and "), nil
}
