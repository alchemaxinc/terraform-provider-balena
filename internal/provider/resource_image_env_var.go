package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NewImageEnvVarResource returns the balena_image_env_var resource.
func NewImageEnvVarResource() resource.Resource {
	return newChildResource(childResourceConfig{
		typeSuffix:       "image_env_var",
		description:      "Manages an environment variable scoped to a specific release_image.",
		parentAttrName:   "release_image_id",
		parentAttrDesc:   "Numeric ID of the parent release_image (release + image pair).",
		keyAttrName:      "name",
		keyAttrDesc:      "Environment variable name.",
		keyValidators:    []validator.String{envVarNameValidator},
		valueSensitive:   true,
		valueDescription: "Environment variable value.",
		api: childResourceAPI{
			create: func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (int64, error) {
				r, err := c.CreateImageEnvVar(ctx, parentID, key, value)
				if err != nil {
					return 0, err
				}
				return r.ID, nil
			},
			read: func(ctx context.Context, c *balena.Client, id int64) (int64, string, string, error) {
				r, err := c.GetImageEnvVar(ctx, id)
				if err != nil {
					return 0, "", "", err
				}
				return r.ReleaseImage.ID, r.Name, r.Value, nil
			},
			update: (*balena.Client).UpdateImageEnvVar,
			delete: (*balena.Client).DeleteImageEnvVar,
		},
	})
}
