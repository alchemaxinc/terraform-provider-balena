package provider

import (
	"context"
	"fmt"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &ReleaseImageDataSource{}

// ReleaseImageDataSource implements the balena_release_image data source.
type ReleaseImageDataSource struct {
	client *balena.Client
}

// ReleaseImageDataSourceModel describes the data model for a release_image lookup.
type ReleaseImageDataSourceModel struct {
	ID        types.Int64 `tfsdk:"id"`
	ReleaseID types.Int64 `tfsdk:"release_id"`
	ImageID   types.Int64 `tfsdk:"image_id"`
}

// NewReleaseImageDataSource returns a new release_image data source instance.
func NewReleaseImageDataSource() datasource.DataSource {
	return &ReleaseImageDataSource{}
}

func (d *ReleaseImageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_release_image"
}

func (d *ReleaseImageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up the release_image pivot linking a release to an image. Useful when managing balena_image_env_var.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Numeric identifier of the release_image.",
				Computed:    true,
			},
			"release_id": schema.Int64Attribute{
				Description: "Numeric identifier of the release.",
				Required:    true,
			},
			"image_id": schema.Int64Attribute{
				Description: "Numeric identifier of the image.",
				Required:    true,
			},
		},
	}
}

func (d *ReleaseImageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, ok := configureClient(req.ProviderData, &resp.Diagnostics, "Data Source")
	if !ok {
		return
	}
	d.client = client
}

func (d *ReleaseImageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ReleaseImageDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ri, err := d.client.GetReleaseImage(ctx, config.ReleaseID.ValueInt64(), config.ImageID.ValueInt64())
	if err != nil {
		if balena.IsNotFound(err) {
			resp.Diagnostics.AddError("release_image not found", fmt.Sprintf("No release_image found for release %d and image %d", config.ReleaseID.ValueInt64(), config.ImageID.ValueInt64()))
			return
		}
		resp.Diagnostics.AddError("Error reading release_image", err.Error())
		return
	}
	config.ID = types.Int64Value(ri.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
