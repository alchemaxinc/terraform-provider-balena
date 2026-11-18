//go:build !integration

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestCreateKeepsTheRowWhenTheReadBackFails checks that a create whose read-back fails leaves a tainted resource
// that the next apply replaces, instead of a row Terraform forgets.
func TestCreateKeepsTheRowWhenTheReadBackFails(t *testing.T) {
	f := newFakePine(t)
	srv := f.server()
	defer srv.Close()
	app := f.seed("application", nil)
	f.getByIDStatus["application_tag"] = http.StatusInternalServerError
	config := providerBlock(srv.URL) + fmt.Sprintf(`
resource "balena_application_tag" "test" {
  application_id = %d
  tag_key        = "k"
  value          = "v"
}
`, app)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{Config: config, ExpectError: regexp.MustCompile(`Error reading application_tag`)},
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					delete(f.getByIDStatus, "application_tag")
				},
				Config: config,
				Check: func(*terraform.State) error {
					if n := f.count("application_tag"); n != 1 {
						return fmt.Errorf("%d application_tag rows, want 1: the first create was orphaned", n)
					}
					return nil
				},
			},
		},
	})
}

// TestDataSourceErrorsHideSensitiveFilters checks that a transport error of a data source that selects on a
// sensitive value does not repeat the value.
func TestDataSourceErrorsHideSensitiveFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	sawError := false
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		ErrorCheck: func(err error) error {
			sawError = true
			if strings.Contains(err.Error(), "s3cret") {
				return fmt.Errorf("the error leaks the sensitive filter: %v", err)
			}
			if !strings.Contains(err.Error(), "Error reading application_environment_variable") {
				return err
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: providerBlock(url) + `
data "balena_application_environment_variable" "test" {
  name  = "TOKEN"
  value = "s3cret-value"
}
`,
		}},
	})
	if !sawError {
		t.Error("expected the data source to fail against a closed server")
	}
}

// TestDateTimesNeverDrift checks that a date-time the API stores in another notation does not show as a change.
func TestDateTimesNeverDrift(t *testing.T) {
	f := newFakePine(t)
	srv := f.server()
	defer srv.Close()
	f.seed("device_type", map[string]any{"slug": "raspberrypi5"})
	org := f.seed("organization", nil)
	f.canonical["is_accessible_by_support_until__date"] = func(v any) any {
		parsed, err := time.Parse(time.RFC3339Nano, v.(string))
		if err != nil {
			return v
		}
		return parsed.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	config := providerBlock(srv.URL) + fmt.Sprintf(`
resource "balena_application" "test" {
  app_name                       = "fleet"
  device_type                    = "raspberrypi5"
  organization_id                = %d
  is_accessible_by_support_until = "2026-12-01T01:00:00+01:00"
}
`, org)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("balena_application.test", "is_accessible_by_support_until", "2026-12-01T01:00:00+01:00"),
					func(*terraform.State) error {
						if p := f.patches("application"); len(p) != 0 {
							return fmt.Errorf("unexpected PATCHes %v", p)
						}
						return nil
					},
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

// TestDerivedValuesAreReadBackAfterUpdate checks that a column the API derives from another (the slug from the name)
// holds the new value right after the update, not the prior one.
func TestDerivedValuesAreReadBackAfterUpdate(t *testing.T) {
	f := newFakePine(t)
	srv := f.server()
	defer srv.Close()
	f.seed("device_type", map[string]any{"slug": "raspberrypi5"})
	org := f.seed("organization", nil)
	f.afterWrite["application"] = func(row map[string]any) {
		row["slug"] = fmt.Sprintf("org/%v", row["app_name"])
	}
	config := func(name string) string {
		return providerBlock(srv.URL) + fmt.Sprintf(`
resource "balena_application" "test" {
  app_name        = %q
  device_type     = "raspberrypi5"
  organization_id = %d
}
`, name, org)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{Config: config("first"), Check: resource.TestCheckResourceAttr("balena_application.test", "slug", "org/first")},
			{Config: config("second"), Check: resource.TestCheckResourceAttr("balena_application.test", "slug", "org/second")},
		},
	})
}

// TestLengthsCountCharacters checks that SBVR lengths are checked in characters, not bytes.
func TestLengthsCountCharacters(t *testing.T) {
	check := func(a *AttributeSpec, value string) bool {
		resp := &validator.StringResponse{}
		for _, v := range stringValidators(a) {
			v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("x"), ConfigValue: types.StringValue(value)}, resp)
		}
		return !resp.Diagnostics.HasError()
	}
	atMost3 := &AttributeSpec{MaxLength: 3}
	atLeast3 := &AttributeSpec{MinLength: 3}
	between := &AttributeSpec{MinLength: 2, MaxLength: 3}
	for _, tc := range []struct {
		a     *AttributeSpec
		value string
		valid bool
	}{
		{atMost3, "ééé", true},   // 3 characters, 6 bytes
		{atMost3, "éééé", false}, // 4 characters
		{atLeast3, "éé", false},  // 2 characters, 4 bytes
		{atLeast3, "ééé", true},
		{between, "日本", true}, // 2 characters, 6 bytes
		{between, "日", false},
	} {
		if got := check(tc.a, tc.value); got != tc.valid {
			t.Errorf("%+v %q valid = %v, want %v", tc.a, tc.value, got, tc.valid)
		}
	}
	dateTime := &AttributeSpec{DateTime: true}
	if !check(dateTime, "2026-12-01T01:00:00+01:00") || check(dateTime, "2026-12-01") {
		t.Error("date-times must be RFC 3339")
	}
}
