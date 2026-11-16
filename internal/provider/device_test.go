//go:build !integration

package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const FAKE_DEVICE_UUID = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"

// fakeDevice seeds a device of fleet 3 with two releases and two host OS releases, and answers the host OS lambda
// filter of deviceHostOSRelease.
type fakeDevice struct {
	*fakePine
	device, commitOne, commitTwo, os600, os610 int64
}

func newFakeDevice(t *testing.T) *fakeDevice {
	f := &fakeDevice{fakePine: newFakePine(t)}
	dt := f.seed("device_type", map[string]any{"slug": "raspberrypi5"})
	fleet := f.seed("application", nil)
	f.commitOne = f.seed("release", map[string]any{"commit": "commit-one", "status": "success", "belongs_to__application": fleet})
	f.commitTwo = f.seed("release", map[string]any{"commit": "commit-two", "status": "success", "belongs_to__application": fleet})
	// A failed release can share its commit with a successful one; it must never be pinned.
	f.seed("release", map[string]any{"commit": "commit-two", "status": "failed", "belongs_to__application": fleet})
	f.os600 = f.seed("release", map[string]any{"raw_version": "6.0.0+rev1", "status": "success", "host_for": "raspberrypi5"})
	f.os610 = f.seed("release", map[string]any{"raw_version": "6.1.0+rev1", "status": "success", "host_for": "raspberrypi5"})
	f.device = f.seed("device", map[string]any{
		"uuid": FAKE_DEVICE_UUID, "device_name": "old-name", "belongs_to__application": fleet,
		"is_of__device_type": dt, "is_pinned_on__release": nil, "should_be_operated_by__release": f.os600,
	})
	f.lambda = func(table, term string, row map[string]any) bool {
		m := regexp.MustCompile(`dt/slug eq '([^']*)'`).FindStringSubmatch(term)
		return table == "release" && m != nil && row["host_for"] == m[1] &&
			strings.HasPrefix(term, "belongs_to__application/any(a:a/is_host eq true and a/is_for__device_type/any(")
	}
	return f
}

func (f *fakeDevice) check(name string, pinned any, hostOS int64) resource.TestCheckFunc {
	return func(*terraform.State) error {
		row := f.row("device", f.device)
		if row["device_name"] != name || fmt.Sprint(row["is_pinned_on__release"]) != fmt.Sprint(pinned) || row["should_be_operated_by__release"] != hostOS {
			return fmt.Errorf("device = %v, want name %s, pin %v, host OS %d", row, name, pinned, hostOS)
		}
		return nil
	}
}

func deviceConfig(url, attributes string) string {
	return providerBlock(url) + fmt.Sprintf(`
resource "balena_device" "test" {
  uuid = %q
%s
}
`, FAKE_DEVICE_UUID, attributes)
}

// TestDeviceAdoptsPinsUnpinsAndUpdatesHostOS covers balena_device's adopt-only behaviour: creating it takes over an
// existing device, a commit pins the successful release of the device's fleet, null unpins, a host OS version
// resolves among host OS releases of the device type, and destroying it leaves the device in place.
func TestDeviceAdoptsPinsUnpinsAndUpdatesHostOS(t *testing.T) {
	f := newFakeDevice(t)
	srv := f.server()
	defer srv.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{
				Config: deviceConfig(srv.URL, `
  device_name                   = "new-name"
  is_pinned_on_release          = "commit-two"
  should_be_operated_by_release = "6.1.0+rev1"
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					f.check("new-name", f.commitTwo, f.os610),
					resource.TestCheckResourceAttr("balena_device.test", "id", fmt.Sprint(f.device)),
					resource.TestCheckResourceAttr("balena_device.test", "is_pinned_on_release", "commit-two"),
					resource.TestCheckResourceAttr("balena_device.test", "should_be_operated_by_release", "6.1.0+rev1"),
				),
			},
			{
				// A null pin unpins the device; a null name or host OS keeps the current value.
				Config: deviceConfig(srv.URL, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					f.check("new-name", nil, f.os610),
					resource.TestCheckNoResourceAttr("balena_device.test", "is_pinned_on_release"),
					resource.TestCheckResourceAttr("balena_device.test", "device_name", "new-name"),
					resource.TestCheckResourceAttr("balena_device.test", "should_be_operated_by_release", "6.1.0+rev1"),
					func(*terraform.State) error {
						patches := f.patches("device")
						last := patches[len(patches)-1]
						if _, ok := last["is_pinned_on__release"]; len(last) != 1 || !ok {
							return fmt.Errorf("last PATCH = %v, want only is_pinned_on__release", last)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "balena_device.test",
				ImportState:       true,
				ImportStateId:     FAKE_DEVICE_UUID,
				ImportStateVerify: true,
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if f.row("device", f.device) == nil {
				return fmt.Errorf("destroying balena_device deleted the device")
			}
			return nil
		},
	})
}

func TestDeviceUnknownHostOSVersionFails(t *testing.T) {
	f := newFakeDevice(t)
	srv := f.server()
	defer srv.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config:      deviceConfig(srv.URL, `  should_be_operated_by_release = "9.9.9"`),
			ExpectError: regexp.MustCompile(`host OS release "9.9.9" not found`),
		}},
	})
}

func TestDeviceAdoptNeedsAnExistingDevice(t *testing.T) {
	f := newFakeDevice(t)
	srv := f.server()
	defer srv.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config:      providerBlock(srv.URL) + `resource "balena_device" "test" { uuid = "ffff" }`,
			ExpectError: regexp.MustCompile(`device with uuid "ffff" not found`),
		}},
	})
}
