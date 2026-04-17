package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NewDeviceTagResource returns the balena_device_tag resource.
func NewDeviceTagResource() resource.Resource {
	return newChildResource(childResourceConfig{
		typeSuffix:       "device_tag",
		description:      "Manages a tag on a Balena device.",
		parentAttrName:   "device_id",
		parentAttrDesc:   "Numeric ID of the parent device.",
		keyAttrName:      "tag_key",
		keyAttrDesc:      "Tag key.",
		keyValidators:    []validator.String{tagKeyValidator},
		valueDescription: "Tag value.",
		api: childResourceAPI{
			create: func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (int64, error) {
				r, err := c.CreateDeviceTag(ctx, parentID, key, value)
				if err != nil {
					return 0, err
				}
				return r.ID, nil
			},
			read: func(ctx context.Context, c *balena.Client, id int64) (int64, string, string, error) {
				r, err := c.GetDeviceTag(ctx, id)
				if err != nil {
					return 0, "", "", err
				}
				return r.Device.ID, r.TagKey, r.Value, nil
			},
			update: (*balena.Client).UpdateDeviceTag,
			delete: (*balena.Client).DeleteDeviceTag,
		},
	})
}
