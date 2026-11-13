//go:build !integration

package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	FAKE_DEVICE_ID   = 7
	FAKE_DEVICE_UUID = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
)

// fakeDeviceAPI serves the device and release endpoints that balena_device calls, and keeps the device in memory.
type fakeDeviceAPI struct {
	mutex            sync.Mutex
	deviceName       string
	pinnedReleaseID  *int64
	hostOSReleaseID  int64
	releaseCommits   map[int64]string
	hostOSVersions   map[int64]string
	patchedFieldSets [][]string
}

func newFakeDeviceAPI() *fakeDeviceAPI {
	return &fakeDeviceAPI{
		deviceName:      "old-name",
		hostOSReleaseID: 600,
		releaseCommits:  map[int64]string{101: "commit-one", 102: "commit-two"},
		hostOSVersions:  map[int64]string{600: "6.0.0+rev1", 610: "6.1.0+rev1"},
	}
}

func (f *fakeDeviceAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v6/device":
		f.writeDevice(w)
	case r.Method == http.MethodPatch && r.URL.Path == fmt.Sprintf("/v6/device(%d)", FAKE_DEVICE_ID):
		f.patchDevice(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v6/release":
		f.writeRelease(w, r.URL.Query().Get("$filter"))
	default:
		http.Error(w, "unexpected request "+r.Method+" "+r.URL.String(), http.StatusNotImplemented)
	}
}

func (f *fakeDeviceAPI) writeDevice(w http.ResponseWriter) {
	pinnedRelease := []map[string]string{}
	if f.pinnedReleaseID != nil {
		pinnedRelease = append(pinnedRelease, map[string]string{"commit": f.releaseCommits[*f.pinnedReleaseID]})
	}
	device := map[string]interface{}{
		"id":                             FAKE_DEVICE_ID,
		"uuid":                           FAKE_DEVICE_UUID,
		"device_name":                    f.deviceName,
		"belongs_to__application":        map[string]int64{"__id": 3},
		"is_of__device_type":             []map[string]string{{"slug": "raspberrypi5"}},
		"should_be_running__release":     pinnedRelease,
		"should_be_operated_by__release": []map[string]string{{"raw_version": f.hostOSVersions[f.hostOSReleaseID]}},
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"d": []interface{}{device}})
}

func (f *fakeDeviceAPI) patchDevice(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var fields map[string]interface{}
	_ = json.Unmarshal(body, &fields)

	var fieldNames []string
	for name, value := range fields {
		fieldNames = append(fieldNames, name)
		switch name {
		case "device_name":
			f.deviceName = value.(string)
		case "should_be_running__release":
			if value == nil {
				f.pinnedReleaseID = nil
			} else {
				releaseID := int64(value.(float64))
				f.pinnedReleaseID = &releaseID
			}
		case "should_be_operated_by__release":
			f.hostOSReleaseID = int64(value.(float64))
		}
	}
	f.patchedFieldSets = append(f.patchedFieldSets, fieldNames)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeDeviceAPI) writeRelease(w http.ResponseWriter, filter string) {
	releases := []map[string]int64{}
	for id, commit := range f.releaseCommits {
		if strings.Contains(filter, "belongs_to__application eq 3 and commit eq '"+commit+"' and status eq 'success'") {
			releases = append(releases, map[string]int64{"id": id})
		}
	}
	for id, version := range f.hostOSVersions {
		if strings.Contains(filter, "raw_version eq '"+version+"'") && strings.Contains(filter, "dt/slug eq 'raspberrypi5'") {
			releases = append(releases, map[string]int64{"id": id})
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"d": releases})
}

// checkFakeDevice checks the device that the fake API holds, and the field sets of the last PATCH.
func (f *fakeDeviceAPI) checkFakeDevice(name string, pinnedReleaseID *int64, hostOSReleaseID int64) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		f.mutex.Lock()
		defer f.mutex.Unlock()
		if f.deviceName != name {
			return fmt.Errorf("device name = %q, want %q", f.deviceName, name)
		}
		if (f.pinnedReleaseID == nil) != (pinnedReleaseID == nil) || (pinnedReleaseID != nil && *f.pinnedReleaseID != *pinnedReleaseID) {
			return fmt.Errorf("pinned release = %v, want %v", f.pinnedReleaseID, pinnedReleaseID)
		}
		if f.hostOSReleaseID != hostOSReleaseID {
			return fmt.Errorf("host OS release = %d, want %d", f.hostOSReleaseID, hostOSReleaseID)
		}
		return nil
	}
}

func deviceConfig(apiURL, attributes string) string {
	return fmt.Sprintf(`
provider "balena" {
  api_url     = %q
  api_token   = "test-token"
  max_retries = 0
}

resource "balena_device" "test" {
  uuid = %q
%s
}
`, apiURL, FAKE_DEVICE_UUID, attributes)
}

func TestDeviceResource_PinsUnpinsAndUpdatesHostOS(t *testing.T) {
	api := newFakeDeviceAPI()
	server := httptest.NewServer(api)
	defer server.Close()
	pinnedReleaseID := int64(102)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"balena": providerserver.NewProtocol6WithError(New("test")()),
		},
		Steps: []resource.TestStep{
			{
				Config: deviceConfig(server.URL, `
  device_name    = "new-name"
  pinned_release = "commit-two"
  os_version     = "6.1.0+rev1"
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					api.checkFakeDevice("new-name", &pinnedReleaseID, 610),
					resource.TestCheckResourceAttr("balena_device.test", "id", "7"),
					resource.TestCheckResourceAttr("balena_device.test", "pinned_release", "commit-two"),
					resource.TestCheckResourceAttr("balena_device.test", "os_version", "6.1.0+rev1"),
				),
			},
			{
				// A null pinned_release unpins the device, and a null os_version or device_name keeps the current value.
				Config: deviceConfig(server.URL, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					api.checkFakeDevice("new-name", nil, 610),
					resource.TestCheckNoResourceAttr("balena_device.test", "pinned_release"),
					resource.TestCheckResourceAttr("balena_device.test", "device_name", "new-name"),
					resource.TestCheckResourceAttr("balena_device.test", "os_version", "6.1.0+rev1"),
				),
			},
			{
				ResourceName:      "balena_device.test",
				ImportState:       true,
				ImportStateId:     FAKE_DEVICE_UUID,
				ImportStateVerify: true,
			},
		},
	})

	if last := api.patchedFieldSets[len(api.patchedFieldSets)-1]; len(last) != 1 || last[0] != "should_be_running__release" {
		t.Errorf("last PATCH fields = %v, want [should_be_running__release]", last)
	}
}

func TestDeviceResource_UnknownHostOSVersionFails(t *testing.T) {
	server := httptest.NewServer(newFakeDeviceAPI())
	defer server.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"balena": providerserver.NewProtocol6WithError(New("test")()),
		},
		Steps: []resource.TestStep{
			{
				Config:      deviceConfig(server.URL, `  os_version = "9.9.9"`),
				ExpectError: regexp.MustCompile(`host OS release "9.9.9" not found`),
			},
		},
	})
}
