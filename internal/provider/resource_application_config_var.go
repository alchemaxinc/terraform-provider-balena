package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NewApplicationConfigVarResource returns the balena_application_config_var resource.
func NewApplicationConfigVarResource() resource.Resource {
	return newChildResource(childResourceConfig{
		typeSuffix:       "application_config_var",
		description:      "Manages an application-level configuration variable (RESIN_/BALENA_ host config) on a Balena fleet.",
		parentAttrName:   "application_id",
		parentAttrDesc:   "Numeric ID of the parent application/fleet.",
		keyAttrName:      "name",
		keyAttrDesc:      "Configuration variable name (e.g. RESIN_HOST_CONFIG_gpu_mem).",
		keyValidators:    []validator.String{configVarNameValidator},
		valueDescription: "Configuration variable value.",
		api: childResourceAPI{
			create: func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (int64, error) {
				r, err := c.CreateApplicationConfigVar(ctx, parentID, key, value)
				if err != nil {
					return 0, err
				}
				return r.ID, nil
			},
			read: func(ctx context.Context, c *balena.Client, id int64) (int64, string, string, error) {
				r, err := c.GetApplicationConfigVar(ctx, id)
				if err != nil {
					return 0, "", "", err
				}
				return r.App.ID, r.Name, r.Value, nil
			},
			update: (*balena.Client).UpdateApplicationConfigVar,
			delete: (*balena.Client).DeleteApplicationConfigVar,
		},
	})
}
