package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NewApplicationTagResource returns the balena_application_tag resource.
func NewApplicationTagResource() resource.Resource {
	return newChildResource(childResourceConfig{
		typeSuffix:       "application_tag",
		description:      "Manages a tag on a Balena application/fleet.",
		parentAttrName:   "application_id",
		parentAttrDesc:   "Numeric ID of the parent application/fleet.",
		keyAttrName:      "tag_key",
		keyAttrDesc:      "Tag key.",
		keyValidators:    []validator.String{tagKeyValidator},
		valueDescription: "Tag value.",
		api: childResourceAPI{
			create: func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (int64, error) {
				r, err := c.CreateApplicationTag(ctx, parentID, key, value)
				if err != nil {
					return 0, err
				}
				return r.ID, nil
			},
			read: func(ctx context.Context, c *balena.Client, id int64) (int64, string, string, error) {
				r, err := c.GetApplicationTag(ctx, id)
				if err != nil {
					return 0, "", "", err
				}
				return r.App.ID, r.TagKey, r.Value, nil
			},
			update: (*balena.Client).UpdateApplicationTag,
			delete: (*balena.Client).DeleteApplicationTag,
		},
	})
}
