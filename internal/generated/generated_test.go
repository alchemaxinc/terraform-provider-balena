//go:build !integration

package generated

import (
	"context"
	"testing"

	"github.com/alchemaxinc/terraform-provider-balena/internal/engine"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// surfaceProvider registers every generated resource and data source, the
// way the real provider will once phase 2 enables them.
type surfaceProvider struct{}

func (surfaceProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "balena"
}
func (surfaceProvider) Schema(context.Context, provider.SchemaRequest, *provider.SchemaResponse) {}
func (surfaceProvider) Configure(context.Context, provider.ConfigureRequest, *provider.ConfigureResponse) {
}
func (surfaceProvider) Resources(context.Context) []func() resource.Resource {
	return engine.ResourceFactories(RESOURCES)
}
func (surfaceProvider) DataSources(context.Context) []func() datasource.DataSource {
	return engine.DataSourceFactories(RESOURCES)
}

// TestGeneratedSurfaceIsValid asks the plugin framework for the full provider
// schema, which validates every generated type name and attribute.
func TestGeneratedSurfaceIsValid(t *testing.T) {
	ctx := context.Background()
	server := providerserver.NewProtocol6(surfaceProvider{})()
	resp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		t.Errorf("%s: %s: %s", d.Severity, d.Summary, d.Detail)
	}
	if got, want := len(resp.ResourceSchemas), len(RESOURCES); got != want {
		t.Errorf("%d resource schemas, want %d", got, want)
	}
	if got, want := len(resp.DataSourceSchemas), len(RESOURCES); got != want {
		t.Errorf("%d data source schemas, want %d", got, want)
	}
}

// TestGeneratedSchemasValidate runs the framework's implementation checks on
// every generated schema.
func TestGeneratedSchemasValidate(t *testing.T) {
	ctx := context.Background()
	for _, d := range RESOURCES {
		var rs resource.SchemaResponse
		engine.NewResource(d).Schema(ctx, resource.SchemaRequest{}, &rs)
		for _, diag := range rs.Schema.ValidateImplementation(ctx) {
			t.Errorf("resource %s: %s: %s", d.TypeName, diag.Summary(), diag.Detail())
		}
		var ds datasource.SchemaResponse
		engine.NewDataSource(d).Schema(ctx, datasource.SchemaRequest{}, &ds)
		for _, diag := range ds.Schema.ValidateImplementation(ctx) {
			t.Errorf("data source %s: %s: %s", d.TypeName, diag.Summary(), diag.Detail())
		}
	}
}
