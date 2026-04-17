package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NewApplicationServiceEnvVarResource returns the balena_application_service_env_var resource.
func NewApplicationServiceEnvVarResource() resource.Resource {
	return newChildResource(childResourceConfig{
		typeSuffix:       "application_service_env_var",
		description:      "Manages a service-level environment variable on a Balena application service.",
		parentAttrName:   "service_id",
		parentAttrDesc:   "Numeric ID of the application service.",
		keyAttrName:      "name",
		keyAttrDesc:      "Environment variable name.",
		keyValidators:    []validator.String{envVarNameValidator},
		valueSensitive:   true,
		valueDescription: "Environment variable value.",
		api: childResourceAPI{
			create: func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (int64, error) {
				r, err := c.CreateServiceEnvVar(ctx, parentID, key, value)
				if err != nil {
					return 0, err
				}
				return r.ID, nil
			},
			read: func(ctx context.Context, c *balena.Client, id int64) (int64, string, string, error) {
				r, err := c.GetServiceEnvVar(ctx, id)
				if err != nil {
					return 0, "", "", err
				}
				return r.Service.ID, r.Name, r.Value, nil
			},
			update: (*balena.Client).UpdateServiceEnvVar,
			delete: (*balena.Client).DeleteServiceEnvVar,
		},
	})
}
