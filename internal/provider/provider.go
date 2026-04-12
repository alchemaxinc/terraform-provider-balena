// Package provider implements the Terraform provider for Balena Cloud.
package provider

import (
	"context"
	"os"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ provider.Provider = &BalenaProvider{}

// BalenaProvider is the Terraform provider implementation.
type BalenaProvider struct {
	version string
}

// BalenaProviderModel describes the provider data model.
type BalenaProviderModel struct {
	APIToken types.String `tfsdk:"api_token"`
	APIURL   types.String `tfsdk:"api_url"`
}

// New returns a provider factory function.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &BalenaProvider{version: version}
	}
}

func (p *BalenaProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "balena"
	resp.Version = p.version
}

func (p *BalenaProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The Balena provider manages Balena Cloud resources via the Balena API.",
		Attributes: map[string]schema.Attribute{
			"api_token": schema.StringAttribute{
				Description: "Balena Cloud API token. May also be set via the BALENA_API_TOKEN environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
			"api_url": schema.StringAttribute{
				Description: "Balena Cloud API URL. Defaults to https://api.balena-cloud.com. May also be set via BALENA_API_URL.",
				Optional:    true,
			},
		},
	}
}

func (p *BalenaProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config BalenaProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiToken := os.Getenv("BALENA_API_TOKEN")
	if !config.APIToken.IsNull() && !config.APIToken.IsUnknown() {
		apiToken = config.APIToken.ValueString()
	}
	if apiToken == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_token"),
			"Missing Balena API Token",
			"The provider requires an API token. Set the api_token attribute or the BALENA_API_TOKEN environment variable.",
		)
		return
	}

	apiURL := os.Getenv("BALENA_API_URL")
	if !config.APIURL.IsNull() && !config.APIURL.IsUnknown() {
		apiURL = config.APIURL.ValueString()
	}

	tflog.Debug(ctx, "Creating Balena API client", map[string]interface{}{"api_url": apiURL})

	client := balena.NewClient(apiURL, apiToken, p.version)
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *BalenaProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewApplicationResource,
		NewApplicationEnvVarResource,
		NewApplicationConfigVarResource,
		NewApplicationServiceEnvVarResource,
		NewApplicationTagResource,
		NewDeviceConfigVarResource,
		NewDeviceEnvVarResource,
		NewDeviceServiceEnvVarResource,
		NewDeviceTagResource,
		NewImageEnvVarResource,
		NewOrganizationResource,
		NewReleaseTagResource,
		NewSSHKeyResource,
		NewServiceLabelResource,
	}
}

func (p *BalenaProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewApplicationDataSource,
		NewDeviceDataSource,
		NewOrganizationDataSource,
		NewReleaseDataSource,
		NewServiceDataSource,
	}
}
