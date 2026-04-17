package provider

import (
	"context"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NewReleaseTagResource returns the balena_release_tag resource.
func NewReleaseTagResource() resource.Resource {
	return newChildResource(childResourceConfig{
		typeSuffix:       "release_tag",
		description:      "Manages a tag on a Balena release.",
		parentAttrName:   "release_id",
		parentAttrDesc:   "Numeric ID of the parent release.",
		keyAttrName:      "tag_key",
		keyAttrDesc:      "Tag key.",
		keyValidators:    []validator.String{tagKeyValidator},
		valueDescription: "Tag value.",
		api: childResourceAPI{
			create: func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (int64, error) {
				r, err := c.CreateReleaseTag(ctx, parentID, key, value)
				if err != nil {
					return 0, err
				}
				return r.ID, nil
			},
			read: func(ctx context.Context, c *balena.Client, id int64) (int64, string, string, error) {
				r, err := c.GetReleaseTag(ctx, id)
				if err != nil {
					return 0, "", "", err
				}
				return r.Release.ID, r.TagKey, r.Value, nil
			},
			update: (*balena.Client).UpdateReleaseTag,
			delete: (*balena.Client).DeleteReleaseTag,
		},
	})
}
