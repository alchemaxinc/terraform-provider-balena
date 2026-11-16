//go:build !integration

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"balena": providerserver.NewProtocol6WithError(New("test")()),
	}
}

func providerBlock(url string) string {
	return fmt.Sprintf(`
provider "balena" {
  api_url     = %q
  api_token   = "test-token"
  max_retries = 0
}
`, url)
}

// stringFor returns a value that satisfies the attribute's validators; variant changes it.
func stringFor(a *AttributeSpec, variant int) string {
	if a.VarName != nil {
		if len(a.VarName.RequiredPrefixes) > 0 {
			return a.VarName.RequiredPrefixes[0] + "HOST_CONFIG_var" + strconv.Itoa(variant)
		}
		return "TEST_VAR_" + strconv.Itoa(variant)
	}
	if len(a.OneOf) > 0 {
		return a.OneOf[variant%len(a.OneOf)]
	}
	n := 8
	if a.MinLength > n {
		n = a.MinLength
	}
	if a.MaxLength > 0 && n > a.MaxLength {
		n = a.MaxLength
	}
	return strings.Repeat(string(rune('a'+variant)), n)
}

// writableConfig seeds the rows the resource references and returns its attribute values as HCL literals.
func writableConfig(f *fakePine, spec *TableSpec) map[string]string {
	values := map[string]string{}
	for _, a := range spec.Attributes {
		if !a.writable() {
			continue
		}
		switch {
		case a.Encoding == ENC_REF:
			values[a.Name] = strconv.FormatInt(f.seed(a.RefTable, nil), 10)
		case a.Encoding == ENC_REF_BY:
			f.seed(a.RefTable, map[string]any{a.RefField: "ref-" + a.Name})
			values[a.Name] = strconv.Quote("ref-" + a.Name)
		case a.Type == ATTR_STRING:
			values[a.Name] = strconv.Quote(stringFor(a, 0))
		case a.Type == ATTR_INT64:
			values[a.Name] = "11"
		case a.Type == ATTR_BOOL:
			values[a.Name] = "true"
		case a.Type == ATTR_FLOAT64:
			values[a.Name] = "1.5"
		}
	}
	return values
}

func block(kind, typ, name string, values map[string]string) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "%s %q %q {\n", kind, "balena_"+typ, name)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s = %s\n", k, values[k])
	}
	b.WriteString("}\n")
	return b.String()
}

func unquote(s string) string {
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}

// TestGeneratedResourcesLifecycle creates, updates, imports and destroys every generated resource that has no create
// hook against the fake API, setting every writable attribute.
func TestGeneratedResourcesLifecycle(t *testing.T) {
	for _, spec := range GENERATED_SPECS {
		if !spec.Resource || spec.Hooks.Create != nil {
			continue
		}
		spec := spec
		t.Run(spec.TypeSuffix, func(t *testing.T) {
			t.Parallel()
			f := newFakePine(t)
			srv := f.server()
			defer srv.Close()
			values := writableConfig(f, spec)
			address := "balena_" + spec.TypeSuffix + ".test"

			var checks []resource.TestCheckFunc
			for name, v := range values {
				checks = append(checks, resource.TestCheckResourceAttr(address, name, unquote(v)))
			}
			checks = append(checks, resource.TestCheckResourceAttrSet(address, "id"))
			steps := []resource.TestStep{{
				Config: providerBlock(srv.URL) + block("resource", spec.TypeSuffix, "test", values),
				Check:  resource.ComposeAggregateTestCheckFunc(checks...),
			}}

			// Update the first plain string attribute that does not force replacement: only it may be patched.
			for _, a := range spec.Attributes {
				if a.writable() && !a.ForceNew && a.Type == ATTR_STRING && a.Encoding == ENC_PLAIN {
					updated := map[string]string{}
					for k, v := range values {
						updated[k] = v
					}
					updated[a.Name] = strconv.Quote(stringFor(a, 1))
					field := a.Field
					steps = append(steps, resource.TestStep{
						Config: providerBlock(srv.URL) + block("resource", spec.TypeSuffix, "test", updated),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(address, a.Name, stringFor(a, 1)),
							func(*terraform.State) error {
								patches := f.patches(spec.Table)
								if len(patches) == 0 {
									return fmt.Errorf("no PATCH sent")
								}
								last := patches[len(patches)-1]
								if len(last) != 1 || last[field] != stringFor(a, 1) {
									return fmt.Errorf("PATCH body = %v, want only %s", last, field)
								}
								return nil
							},
						),
					})
					break
				}
			}
			steps = append(steps, resource.TestStep{ResourceName: address, ImportState: true, ImportStateVerify: true})
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testFactories(),
				Steps:                    steps,
				CheckDestroy: func(*terraform.State) error {
					if n := f.count(spec.Table); n != 0 {
						return fmt.Errorf("%d %s rows left after destroy", n, spec.Table)
					}
					return nil
				},
			})
		})
	}
}

// seedFullRow stores a row with a value in every column of a spec and returns its id and the values the data source
// must read back.
func seedFullRow(f *fakePine, spec *TableSpec) (int64, map[string]string) {
	row := map[string]any{}
	want := map[string]string{}
	for _, a := range spec.Attributes {
		if a.Field == "id" {
			continue
		}
		switch {
		case a.Encoding == ENC_REF:
			id := f.seed(a.RefTable, nil)
			row[a.Field] = id
			want[a.Name] = strconv.FormatInt(id, 10)
		case a.Encoding == ENC_REF_BY:
			row[a.Field] = f.seed(a.RefTable, map[string]any{a.RefField: "ref-" + a.Name})
			want[a.Name] = "ref-" + a.Name
		case a.Encoding == ENC_JSON:
			row[a.Field] = map[string]any{"k": "v"}
			want[a.Name] = `{"k":"v"}`
		case a.Type == ATTR_INT64:
			row[a.Field] = int64(7)
			want[a.Name] = "7"
		case a.Type == ATTR_BOOL:
			row[a.Field] = true
			want[a.Name] = "true"
		case a.Type == ATTR_FLOAT64:
			row[a.Field] = 1.5
			want[a.Name] = "1.5"
		default:
			row[a.Field] = "value-" + a.Name
			want[a.Name] = "value-" + a.Name
		}
	}
	id := f.seed(spec.Table, row)
	want["id"] = strconv.FormatInt(id, 10)
	return id, want
}

// TestGeneratedDataSources reads every generated data source by id and by another attribute.
func TestGeneratedDataSources(t *testing.T) {
	for _, spec := range GENERATED_SPECS {
		if !spec.DataSource {
			continue
		}
		spec := spec
		t.Run(spec.TypeSuffix, func(t *testing.T) {
			t.Parallel()
			f := newFakePine(t)
			srv := f.server()
			defer srv.Close()
			id, want := seedFullRow(f, spec)
			other := ""
			for _, a := range spec.Attributes {
				if a.DataSource == DS_FILTER && a.Name != "id" && a.Type == ATTR_STRING && !a.Sensitive {
					other = a.Name
					break
				}
			}
			config := providerBlock(srv.URL) + block("data", spec.TypeSuffix, "by_id", map[string]string{"id": strconv.FormatInt(id, 10)})
			var checks []resource.TestCheckFunc
			for name, v := range want {
				checks = append(checks, resource.TestCheckResourceAttr("data.balena_"+spec.TypeSuffix+".by_id", name, v))
			}
			if other != "" {
				config += block("data", spec.TypeSuffix, "by_other", map[string]string{other: strconv.Quote(want[other])})
				checks = append(checks, resource.TestCheckResourceAttr("data.balena_"+spec.TypeSuffix+".by_other", "id", want["id"]))
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testFactories(),
				Steps:                    []resource.TestStep{{Config: config, Check: resource.ComposeAggregateTestCheckFunc(checks...)}},
			})
		})
	}
}

func TestDataSourceNeedsExactlyOneRow(t *testing.T) {
	f := newFakePine(t)
	srv := f.server()
	defer srv.Close()
	f.seed("application", map[string]any{"app_name": "twin"})
	f.seed("application", map[string]any{"app_name": "twin"})
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{
			{
				Config:      providerBlock(srv.URL) + `data "balena_application" "x" { app_name = "twin" }`,
				ExpectError: regexp.MustCompile(`More than one application found`),
			},
			{
				Config:      providerBlock(srv.URL) + `data "balena_application" "x" { app_name = "none" }`,
				ExpectError: regexp.MustCompile(`No application found`),
			},
			{
				Config:      providerBlock(srv.URL) + `data "balena_application" "x" {}`,
				ExpectError: regexp.MustCompile(`(?s)At least one of these attributes must be configured`),
			},
		},
	})
}

// TestReadRemovesMissingRow checks that a row deleted outside Terraform (an empty collection on GET by id) is
// planned for re-creation instead of failing.
func TestReadRemovesMissingRow(t *testing.T) {
	f := newFakePine(t)
	srv := f.server()
	defer srv.Close()
	app := f.seed("application", nil)
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
			{Config: config},
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					f.rows["application_tag"] = map[int64]map[string]any{}
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestOrganizationDeleteTolerates401 checks the organization delete hook: the API refuses to delete organizations
// with an API token.
func TestOrganizationDeleteTolerates401(t *testing.T) {
	f := newFakePine(t)
	f.deleteStatus["organization"] = 401
	srv := f.server()
	defer srv.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testFactories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(srv.URL) + `resource "balena_organization" "test" {
  name   = "Org"
  handle = "org_handle"
}`,
			Check: resource.TestCheckResourceAttr("balena_organization.test", "handle", "org_handle"),
		}},
	})
	f.deleteStatus["organization"] = 500
	if err := organizationDeleteTolerates401(context.Background(), &HookContext{Client: balena.NewClient(srv.URL, "t", "test", balena.WithMaxRetries(0)), Spec: specOf("organization")}, 1); err == nil {
		t.Error("a 500 must still fail")
	}
}

func specOf(table string) *TableSpec {
	for _, s := range GENERATED_SPECS {
		if s.Table == table {
			return s
		}
	}
	return nil
}

func TestVarNameValidators(t *testing.T) {
	rule := func(table string) varNameValidator {
		a := specOf(table).attr("name")
		if a == nil || a.VarName == nil {
			t.Fatalf("%s.name has no variable-name rule", table)
		}
		return varNameValidator{rule: a.VarName, invalid: regexp.MustCompile(a.VarName.Invalid)}
	}
	tests := []struct {
		table, name string
		valid       bool
	}{
		{"application_environment_variable", "MY_VAR", true},
		{"application_environment_variable", "_x1", true},
		{"application_environment_variable", "1ABC", false},
		{"application_environment_variable", "MY.VAR", false},
		{"application_environment_variable", "MY:VAR", false},
		{"application_environment_variable", "BALENA_X", false},
		{"application_environment_variable", "USER", false},
		{"device_environment_variable", "RESIN_X", false},
		{"service_environment_variable", "ok", true},
		{"device_service_environment_variable", "ok", true},
		// Configuration variables accept a colon and reject a dot (the old provider had this backwards).
		{"application_config_variable", "BALENA_HOST_CONFIG_dtoverlay", true},
		{"application_config_variable", "RESIN_HOST_CONFIG_gpu:mem", true},
		{"application_config_variable", "BALENA_HOST_CONFIG_a.b", false},
		{"device_config_variable", "MY_CONFIG", false},
		{"device_config_variable", "BALENA", false},
		{"device_config_variable", ":BALENA_X", false},
	}
	for _, tc := range tests {
		if got := rule(tc.table).check(tc.name) == ""; got != tc.valid {
			t.Errorf("%s %q valid = %v, want %v", tc.table, tc.name, got, tc.valid)
		}
	}
	if a := specOf("image_environment_variable").attr("name"); a.VarName != nil {
		t.Errorf("image environment variable names are not checked upstream, so they must not be here")
	}
}

func TestDerivedLengthValidators(t *testing.T) {
	for _, tc := range []struct {
		table, attr string
		min, max    int
	}{
		{"release_tag", "tag_key", 1, 164}, // the old provider capped every tag key at 100
		{"device_tag", "tag_key", 1, 100},
		{"application", "app_name", 4, 100},
		{"application_profile", "profile_name", 2, 100},
		{"user_public_key", "public_key", 0, 2850},
	} {
		a := specOf(tc.table).attr(tc.attr)
		if a == nil || a.MinLength != tc.min || a.MaxLength != tc.max {
			t.Errorf("%s.%s length = %+v, want %d..%d", tc.table, tc.attr, a, tc.min, tc.max)
		}
	}
	if a := specOf("application").attr("class"); strings.Join(a.OneOf, ",") != "fleet,block,app" {
		t.Errorf("application.class one_of = %v", a.OneOf)
	}
}

// TestHooksUseOnlyDeclaredColumns runs every hook against the fake API and checks that each column it names in a
// request is declared for it in schema/overrides.yaml (GENERATED_HOOK_USES), which the generator checks against the IR.
func TestHooksUseOnlyDeclaredColumns(t *testing.T) {
	f := newFakePine(t)
	srv := f.server()
	defer srv.Close()
	dt := f.seed("device_type", map[string]any{"slug": "raspberrypi5"})
	fleet := f.seed("application", nil)
	device := f.seed("device", map[string]any{"uuid": "abc", "belongs_to__application": fleet, "is_of__device_type": dt})
	f.seed("release", map[string]any{"commit": "c1", "status": "success", "belongs_to__application": fleet})
	f.seed("release", map[string]any{"raw_version": "6.0.0+rev1", "status": "success"})
	f.lambda = func(string, string, map[string]any) bool { return true }
	hc := &HookContext{Client: balena.NewClient(srv.URL, "t", "test", balena.WithMaxRetries(0)), Spec: specOf("device")}
	ctx := context.Background()
	run := map[string]func() error{
		"deviceAdopt": func() error {
			_, err := deviceAdopt(ctx, hc, map[string]attr.Value{"uuid": types.StringValue("abc")})
			return err
		},
		"deviceForget":        func() error { return deviceForget(ctx, hc, device) },
		"devicePinnedRelease": func() error { _, err := devicePinnedRelease(ctx, hc, device, "c1"); return err },
		"deviceHostOSRelease": func() error { _, err := deviceHostOSRelease(ctx, hc, device, "6.0.0+rev1"); return err },
		"organizationDeleteTolerates401": func() error {
			return organizationDeleteTolerates401(ctx, &HookContext{Client: hc.Client, Spec: specOf("organization")}, f.seed("organization", nil))
		},
	}
	ident := regexp.MustCompile(`[a-z][a-z_]*[a-z]`)
	quoted := regexp.MustCompile(`'(?:[^']|'')*'`)
	keywords := map[string]bool{"eq": true, "and": true, "any": true, "true": true, "false": true, "null": true, "dt": true}
	for name := range GENERATED_HOOK_USES {
		if run[name] == nil {
			t.Errorf("no test drives hook %s", name)
		}
	}
	for name, call := range run {
		declared := map[string]bool{}
		for _, use := range GENERATED_HOOK_USES[name] {
			_, field, _ := strings.Cut(use, ".")
			declared[field] = true
		}
		f.mu.Lock()
		f.requests = nil
		f.mu.Unlock()
		if err := call(); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		f.mu.Lock()
		for _, r := range f.requests {
			var text []string
			for k, v := range r.Query {
				if k == "$select" || k == "$expand" || k == "$filter" {
					text = append(text, strings.ReplaceAll(v, "$select", ""))
				}
			}
			for k := range r.Body {
				text = append(text, k)
			}
			for _, word := range ident.FindAllString(quoted.ReplaceAllString(strings.Join(text, " "), ""), -1) {
				if !keywords[word] && !declared[word] {
					t.Errorf("hook %s uses column %s on %s, which overrides.yaml does not declare", name, word, r.Table)
				}
			}
		}
		f.mu.Unlock()
	}
}

// TestJSONValuesAreStable checks that JSON columns read back as compact JSON strings.
func TestJSONValuesAreStable(t *testing.T) {
	a := &AttributeSpec{Field: "composition", Encoding: ENC_JSON, Type: ATTR_STRING}
	var row balena.Row
	_ = json.Unmarshal([]byte(`{"composition":{"b":1,"a":[1,2]}}`), &row)
	v, err := fromAPI(a, row)
	if err != nil || v.(types.String).ValueString() != `{"a":[1,2],"b":1}` {
		t.Errorf("composition = %v, %v", v, err)
	}
}
