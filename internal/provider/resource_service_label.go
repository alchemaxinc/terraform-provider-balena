package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NewServiceLabelResource returns the balena_service_label resource.
func NewServiceLabelResource() resource.Resource {
	return newChildResource(childResourceConfig{
		typeSuffix:       "service_label",
		description:      "Manages a label on a Balena application service.",
		parentAttrName:   "service_id",
		parentAttrDesc:   "Numeric ID of the parent service.",
		keyAttrName:      "label_name",
		keyAttrDesc:      "Label name (e.g. io.balena.features.dbus).",
		keyValidators:    []validator.String{labelNameValidator},
		valueDescription: "Label value.",
		api: childResourceAPI{
			create: func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (int64, error) {
				r, err := c.CreateServiceLabel(ctx, parentID, key, value)
				if err != nil {
					return 0, err
				}
				return r.ID, nil
			},
			read: func(ctx context.Context, c *balena.Client, id int64) (int64, string, string, error) {
				r, err := c.GetServiceLabel(ctx, id)
				if err != nil {
					return 0, "", "", err
				}
				return r.Service.ID, r.LabelName, r.Value, nil
			},
			update: (*balena.Client).UpdateServiceLabel,
			delete: (*balena.Client).DeleteServiceLabel,
		},
	})
}
