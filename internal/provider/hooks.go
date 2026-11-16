package provider

import (
	"context"
	"fmt"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Hand-written hooks. Each is named in schema/overrides.yaml, which also lists the API columns ("<table>.<field>") the
// hook uses; the generator checks those against the IR and TestHooksUseOnlyDeclaredColumns checks the hooks against
// that list.

// plannedField returns the planned value of the attribute that maps to an API field.
func plannedField(spec *TableSpec, planned map[string]attr.Value, field string) (attr.Value, error) {
	for _, a := range spec.Attributes {
		if a.Field == field && a.Encoding == ENC_PLAIN {
			if v, ok := planned[a.Name]; ok && !v.IsNull() && !v.IsUnknown() {
				return v, nil
			}
		}
	}
	return nil, fmt.Errorf("%s needs a known %s", spec.Table, field)
}

// deviceAdopt finds the device a new balena_device takes over, by uuid: an API token cannot create a device.
func deviceAdopt(ctx context.Context, hc *HookContext, planned map[string]attr.Value) (int64, error) {
	uuid, err := plannedField(hc.Spec, planned, "uuid")
	if err != nil {
		return 0, err
	}
	return lookupID(ctx, hc.Client, hc.Spec.Root, "device", balena.Eq("uuid", scalarToAPI(uuid)), "device with uuid "+uuid.String())
}

// deviceForget leaves the device untouched when balena_device is destroyed: Terraform only stops managing it.
func deviceForget(ctx context.Context, _ *HookContext, id int64) error {
	tflog.Info(ctx, "leaving the device in place; Terraform no longer manages it", map[string]any{"device_id": id})
	return nil
}

// devicePinnedRelease resolves a release commit to the successful release of the device's fleet with that commit. A
// failed release can share its commit with a successful one, and only a successful release can be pinned.
func devicePinnedRelease(ctx context.Context, hc *HookContext, id int64, commit string) (int64, error) {
	device, err := hc.Client.Get(ctx, hc.Spec.Root, "device", id, balena.Query{Select: []string{"belongs_to__application"}})
	if err != nil {
		return 0, err
	}
	fleet, ok, err := balena.RefID(device["belongs_to__application"])
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("device %d belongs to no fleet, so it cannot be pinned", id)
	}
	rows, err := hc.Client.List(ctx, hc.Spec.Root, "release", balena.Query{
		Select: []string{"id"},
		Filter: balena.And(balena.Eq("belongs_to__application", fleet), balena.Eq("commit", commit), balena.Eq("status", "success")),
		Top:    1,
	})
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, &balena.APIError{StatusCode: 404, Body: fmt.Sprintf("no successful release with commit %q in fleet %d", commit, fleet)}
	}
	release, _, err := balena.ToInt64(rows[0]["id"])
	return release, err
}

// deviceHostOSRelease resolves a host OS version (raw_version, for example 6.0.10+rev1) to the successful release of
// that version among the host applications for the device's type. The filter navigates relations, which the API
// accepts only with `/`, `(`, `)` and `:` unencoded; balena.EscapeFilter keeps them.
func deviceHostOSRelease(ctx context.Context, hc *HookContext, id int64, version string) (int64, error) {
	device, err := hc.Client.Get(ctx, hc.Spec.Root, "device", id, balena.Query{
		Expand: []balena.Expand{{Field: "is_of__device_type", Select: []string{"slug"}}},
	})
	if err != nil {
		return 0, err
	}
	deviceType, ok, err := balena.Expanded(device["is_of__device_type"])
	if err != nil || !ok {
		return 0, fmt.Errorf("reading the device type of device %d: %v", id, err)
	}
	slug := deviceType["slug"]
	filter := balena.And(
		balena.Eq("raw_version", version),
		balena.Eq("status", "success"),
		"belongs_to__application/any(a:a/is_host eq true and a/is_for__device_type/any(dt:dt/"+balena.Eq("slug", slug)+"))",
	)
	rows, err := hc.Client.List(ctx, hc.Spec.Root, "release", balena.Query{Select: []string{"id"}, Filter: filter, Top: 1})
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, &balena.APIError{StatusCode: 404, Body: fmt.Sprintf("host OS release %q not found for device type %v", version, slug)}
	}
	release, _, err := balena.ToInt64(rows[0]["id"])
	return release, err
}

// organizationDeleteTolerates401 deletes an organization. The API refuses organization deletion to API tokens with a
// 401, so that answer only removes the organization from the state.
func organizationDeleteTolerates401(ctx context.Context, hc *HookContext, id int64) error {
	err := hc.Client.Delete(ctx, hc.Spec.Root, hc.Spec.Table, id)
	if balena.IsUnauthorized(err) {
		tflog.Warn(ctx, "the API does not allow deleting the organization with this token; removing it from the state only", map[string]any{"organization_id": id})
		return nil
	}
	return err
}
