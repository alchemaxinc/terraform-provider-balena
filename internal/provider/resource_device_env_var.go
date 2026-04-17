package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NewDeviceEnvVarResource returns the balena_device_env_var resource.
func NewDeviceEnvVarResource() resource.Resource {
	return newChildResource(childResourceConfig{
		typeSuffix:       "device_env_var",
		description:      "Manages a device-level environment variable.",
		parentAttrName:   "device_id",
		parentAttrDesc:   "Numeric ID of the parent device.",
		keyAttrName:      "name",
		keyAttrDesc:      "Environment variable name.",
		keyValidators:    []validator.String{envVarNameValidator},
		valueSensitive:   true,
		valueDescription: "Environment variable value.",
		api: childResourceAPI{
			create: func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (int64, error) {
				r, err := c.CreateDeviceEnvVar(ctx, parentID, key, value)
				if err != nil {
					return 0, err
				}
				return r.ID, nil
			},
			read: func(ctx context.Context, c *balena.Client, id int64) (int64, string, string, error) {
				r, err := c.GetDeviceEnvVar(ctx, id)
				if err != nil {
					return 0, "", "", err
				}
				return r.Device.ID, r.Name, r.Value, nil
			},
			update: (*balena.Client).UpdateDeviceEnvVar,
			delete: (*balena.Client).DeleteDeviceEnvVar,
		},
	})
}
