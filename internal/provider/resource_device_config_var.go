package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NewDeviceConfigVarResource returns the balena_device_config_var resource.
func NewDeviceConfigVarResource() resource.Resource {
	return newChildResource(childResourceConfig{
		typeSuffix:       "device_config_var",
		description:      "Manages a device-level configuration variable (RESIN_/BALENA_ host config).",
		parentAttrName:   "device_id",
		parentAttrDesc:   "Numeric ID of the parent device.",
		keyAttrName:      "name",
		keyAttrDesc:      "Configuration variable name (e.g. RESIN_HOST_CONFIG_gpu_mem).",
		keyValidators:    []validator.String{configVarNameValidator},
		valueDescription: "Configuration variable value.",
		api: childResourceAPI{
			create: func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (int64, error) {
				r, err := c.CreateDeviceConfigVar(ctx, parentID, key, value)
				if err != nil {
					return 0, err
				}
				return r.ID, nil
			},
			read: func(ctx context.Context, c *balena.Client, id int64) (int64, string, string, error) {
				r, err := c.GetDeviceConfigVar(ctx, id)
				if err != nil {
					return 0, "", "", err
				}
				return r.Device.ID, r.Name, r.Value, nil
			},
			update: (*balena.Client).UpdateDeviceConfigVar,
			delete: (*balena.Client).DeleteDeviceConfigVar,
		},
	})
}
