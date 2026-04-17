package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NewDeviceServiceEnvVarResource returns the balena_device_service_env_var resource.
func NewDeviceServiceEnvVarResource() resource.Resource {
	return newChildResource(childResourceConfig{
		typeSuffix:       "device_service_env_var",
		description:      "Manages a device-service environment variable (scoped to a service_install on a device).",
		parentAttrName:   "service_install_id",
		parentAttrDesc:   "Numeric ID of the parent service_install (device + service pair).",
		keyAttrName:      "name",
		keyAttrDesc:      "Environment variable name.",
		keyValidators:    []validator.String{envVarNameValidator},
		valueSensitive:   true,
		valueDescription: "Environment variable value.",
		api: childResourceAPI{
			create: func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (int64, error) {
				r, err := c.CreateDeviceServiceEnvVar(ctx, parentID, key, value)
				if err != nil {
					return 0, err
				}
				return r.ID, nil
			},
			read: func(ctx context.Context, c *balena.Client, id int64) (int64, string, string, error) {
				r, err := c.GetDeviceServiceEnvVar(ctx, id)
				if err != nil {
					return 0, "", "", err
				}
				return r.ServiceInstall.ID, r.Name, r.Value, nil
			},
			update: (*balena.Client).UpdateDeviceServiceEnvVar,
			delete: (*balena.Client).DeleteDeviceServiceEnvVar,
		},
	})
}
