package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NewApplicationEnvVarResource returns the balena_application_env_var resource.
func NewApplicationEnvVarResource() resource.Resource {
	return newChildResource(childResourceConfig{
		typeSuffix:       "application_env_var",
		description:      "Manages an application-level environment variable on a Balena fleet.",
		parentAttrName:   "application_id",
		parentAttrDesc:   "Numeric ID of the parent application/fleet.",
		keyAttrName:      "name",
		keyAttrDesc:      "Environment variable name.",
		keyValidators:    []validator.String{envVarNameValidator},
		valueSensitive:   true,
		valueDescription: "Environment variable value.",
		api: childResourceAPI{
			create: func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (int64, error) {
				r, err := c.CreateApplicationEnvVar(ctx, parentID, key, value)
				if err != nil {
					return 0, err
				}
				return r.ID, nil
			},
			read: func(ctx context.Context, c *balena.Client, id int64) (int64, string, string, error) {
				r, err := c.GetApplicationEnvVar(ctx, id)
				if err != nil {
					return 0, "", "", err
				}
				return r.App.ID, r.Name, r.Value, nil
			},
			update: (*balena.Client).UpdateApplicationEnvVar,
			delete: (*balena.Client).DeleteApplicationEnvVar,
		},
	})
}
