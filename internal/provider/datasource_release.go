package provider

import (
	"context"
	"fmt"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource                     = &ReleaseDataSource{}
	_ datasource.DataSourceWithConfigValidators = &ReleaseDataSource{}
)

// ReleaseDataSource implements the balena_release data source.
type ReleaseDataSource struct {
	client *balena.Client
}

// ReleaseDataSourceModel describes the release data source data model.
type ReleaseDataSourceModel struct {
	ID             types.Int64  `tfsdk:"id"`
	ApplicationID  types.Int64  `tfsdk:"application_id"`
	Commit         types.String `tfsdk:"commit"`
	Status         types.String `tfsdk:"status"`
	ReleaseVersion types.String `tfsdk:"release_version"`
	Semver         types.String `tfsdk:"semver"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

// NewReleaseDataSource returns a new release data source instance.
func NewReleaseDataSource() datasource.DataSource {
	return &ReleaseDataSource{}
}

func (d *ReleaseDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_release"
}

func (d *ReleaseDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a Balena release by ID or by application ID and commit.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier of the release. Provide either id or (application_id + commit).",
				Optional:    true,
				Computed:    true,
			},
			"application_id": schema.Int64Attribute{
				Description: "ID of the application the release belongs to. Required when looking up by commit.",
				Optional:    true,
				Computed:    true,
			},
			"commit": schema.StringAttribute{
				Description: "Commit hash of the release. Required when looking up by application_id.",
				Optional:    true,
				Computed:    true,
			},
			"status": schema.StringAttribute{
				Description: "Release status.",
				Computed:    true,
			},
			"release_version": schema.StringAttribute{
				Description: "Release version.",
				Computed:    true,
			},
			"semver": schema.StringAttribute{
				Description: "Semantic version.",
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Description: "Creation timestamp.",
				Computed:    true,
			},
		},
	}
}

func (d *ReleaseDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*balena.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *balena.Client")
		return
	}
	d.client = client
}

func (d *ReleaseDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ReleaseDataSourceModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var release *balena.Release
	var err error

	if !config.ID.IsNull() && !config.ID.IsUnknown() {
		release, err = d.client.GetRelease(ctx, config.ID.ValueInt64())
	} else if !config.ApplicationID.IsNull() && !config.ApplicationID.IsUnknown() &&
		!config.Commit.IsNull() && !config.Commit.IsUnknown() {
		release, err = d.client.GetReleaseByCommit(ctx, config.ApplicationID.ValueInt64(), config.Commit.ValueString())
	}

	if err != nil {
		if balena.IsNotFound(err) {
			resp.Diagnostics.AddError("Release not found", fmt.Sprintf("No release matched the given criteria: %s", err.Error()))
			return
		}
		resp.Diagnostics.AddError("Error reading release", fmt.Sprintf("Could not read release: %s", err.Error()))
		return
	}

	config.ID = types.Int64Value(release.ID)
	config.ApplicationID = types.Int64Value(release.App.ID)
	config.Commit = types.StringValue(release.Commit)
	config.Status = types.StringValue(release.Status)
	config.ReleaseVersion = types.StringValue(release.ReleaseVersion)
	config.Semver = types.StringValue(release.Semver)
	config.CreatedAt = types.StringValue(release.CreatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}

// ConfigValidators returns validators that ensure either id or (application_id + commit) is provided.
func (d *ReleaseDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(
			path.MatchRoot("id"),
			path.MatchRoot("commit"),
		),
		datasourcevalidator.RequiredTogether(
			path.MatchRoot("application_id"),
			path.MatchRoot("commit"),
		),
	}
}
