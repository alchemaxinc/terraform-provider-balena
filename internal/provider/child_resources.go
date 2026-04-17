package provider

import (
	"context"
	"regexp"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// envVarNameValidator enforces POSIX-ish environment variable naming: must
// begin with a letter or underscore, followed by letters, digits or underscores.
// Balena additionally reserves the RESIN_ and BALENA_ prefixes for config vars,
// not env vars, but that's validated server-side.
var envVarNameValidator = regexpStringValidator(
	regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`),
	"must start with a letter or underscore and contain only letters, digits, and underscores",
)

// configVarNameValidator allows the dotted host-config keys used by Balena
// (e.g. RESIN_HOST_CONFIG_gpu_mem, BALENA_HOST_CONFIG_dtoverlay).
var configVarNameValidator = regexpStringValidator(
	regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`),
	"must start with a letter or underscore and contain only letters, digits, underscores, and dots",
)

// tagKeyValidator enforces Balena's tag key rules: non-empty, no whitespace,
// max 100 characters. The full rules are enforced server-side.
var tagKeyValidator = regexpStringValidator(
	regexp.MustCompile(`^[^\s]{1,100}$`),
	"must be non-empty, contain no whitespace, and be at most 100 characters",
)

// labelNameValidator matches Docker/Balena service label naming (dotted reverse
// DNS plus hyphens are allowed).
var labelNameValidator = regexpStringValidator(
	regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`),
	"must start with a letter or underscore and contain only letters, digits, underscores, dots, and hyphens",
)

// NewApplicationEnvVarResource returns the application env var resource.
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

// NewApplicationConfigVarResource returns the application config var resource.
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

// NewApplicationServiceEnvVarResource returns the application service env var resource.
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

// NewApplicationTagResource returns the application tag resource.
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

// NewDeviceEnvVarResource returns the device env var resource.
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

// NewDeviceConfigVarResource returns the device config var resource.
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

// NewDeviceServiceEnvVarResource returns the device service env var resource.
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

// NewDeviceTagResource returns the device tag resource.
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

// NewReleaseTagResource returns the release tag resource.
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

// NewImageEnvVarResource returns the image env var resource.
func NewImageEnvVarResource() resource.Resource {
	return newChildResource(childResourceConfig{
		typeSuffix:       "image_env_var",
		description:      "Manages an environment variable scoped to a specific release_image.",
		parentAttrName:   "release_image_id",
		parentAttrDesc:   "Numeric ID of the parent release_image (release + image pair).",
		keyAttrName:      "name",
		keyAttrDesc:      "Environment variable name.",
		keyValidators:    []validator.String{envVarNameValidator},
		valueSensitive:   true,
		valueDescription: "Environment variable value.",
		api: childResourceAPI{
			create: func(ctx context.Context, c *balena.Client, parentID int64, key, value string) (int64, error) {
				r, err := c.CreateImageEnvVar(ctx, parentID, key, value)
				if err != nil {
					return 0, err
				}
				return r.ID, nil
			},
			read: func(ctx context.Context, c *balena.Client, id int64) (int64, string, string, error) {
				r, err := c.GetImageEnvVar(ctx, id)
				if err != nil {
					return 0, "", "", err
				}
				return r.ReleaseImage.ID, r.Name, r.Value, nil
			},
			update: (*balena.Client).UpdateImageEnvVar,
			delete: (*balena.Client).DeleteImageEnvVar,
		},
	})
}

// NewServiceLabelResource returns the service label resource.
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

// regexpStringValidator builds a validator.String that enforces a regexp match.
// The framework's built-in stringvalidator.RegexMatches would be preferable but
// keeping this helper avoids an extra dependency module import.
func regexpStringValidator(re *regexp.Regexp, msg string) validator.String {
	return &regexpValidator{re: re, msg: msg}
}

type regexpValidator struct {
	re  *regexp.Regexp
	msg string
}

func (v *regexpValidator) Description(_ context.Context) string {
	return "value " + v.msg
}

func (v *regexpValidator) MarkdownDescription(_ context.Context) string {
	return "value " + v.msg
}

func (v *regexpValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueString()
	if !v.re.MatchString(val) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid value",
			"Value "+v.msg+", got: "+val,
		)
	}
}
