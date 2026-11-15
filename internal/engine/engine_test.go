//go:build !integration

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// testDescriptor covers every attribute type, codec and mode.
var testDescriptor = Resource{
	TypeName: "widget",
	Path:     "/v6/widget",
	Attributes: []Attribute{
		{Name: "name", Wire: "name", Type: TYPE_STRING, Mode: MODE_REQUIRED, Filterable: true},
		{Name: "belongs_to__thing", Wire: "belongs_to__thing", Type: TYPE_INT64, Codec: CODEC_FOREIGN_KEY, Mode: MODE_REQUIRED, Filterable: true},
		{Name: "note", Wire: "note", Type: TYPE_STRING, Mode: MODE_OPTIONAL, Filterable: true},
		{Name: "is_enabled", Wire: "is_enabled", Type: TYPE_BOOL, Mode: MODE_OPTIONAL_COMPUTED, Filterable: true},
		{Name: "size", Wire: "size", Type: TYPE_INT64, Mode: MODE_COMPUTED, Filterable: true},
		{Name: "settings", Wire: "settings", Type: TYPE_STRING, Codec: CODEC_JSON, Mode: MODE_OPTIONAL},
		{Name: "secret", Wire: "secret", Type: TYPE_STRING, Mode: MODE_OPTIONAL, Sensitive: true, WriteOnly: true},
	},
}

// widgetEntity is what the fake API returns for widget 7.
const widgetEntity = `{"id":7,"name":"w","belongs_to__thing":{"__id":42},"note":null,"is_enabled":true,` +
	`"size":"9007199254740993","settings":{"b":1, "a":[true]},"secret":"$2b$hash"}`

type recorded struct {
	method, path, query string
	body                []byte
}

// fakeAPI answers requests from a map keyed by "METHOD path".
func fakeAPI(t *testing.T, routes map[string]string) (*balena.Client, *[]recorded) {
	t.Helper()
	var reqs []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqs = append(reqs, recorded{r.Method, r.URL.Path, r.URL.RawQuery, body})
		resp, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`"Not Found"`))
			return
		}
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return balena.NewClient(srv.URL, "tok", "test", balena.WithHTTPClient(srv.Client()), balena.WithMaxRetries(0)), &reqs
}

func num(n int64) tftypes.Value  { return tftypes.NewValue(tftypes.Number, new(big.Float).SetInt64(n)) }
func str(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }

func newTestResource(c *balena.Client) *genericResource {
	return &genericResource{d: testDescriptor, client: c}
}

// objectOf builds an object value of the resource schema, filling unset
// attributes with the given default (null or unknown).
func objectOf(t *testing.T, typ tftypes.Type, set map[string]tftypes.Value, unknown bool) tftypes.Value {
	t.Helper()
	obj := typ.(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for name, at := range obj.AttributeTypes {
		if v, ok := set[name]; ok {
			vals[name] = v
		} else if unknown {
			vals[name] = tftypes.NewValue(at, tftypes.UnknownValue)
		} else {
			vals[name] = tftypes.NewValue(at, nil)
		}
	}
	return tftypes.NewValue(typ, vals)
}

func assertBody(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	dec := json.NewDecoder(bytes.NewReader(got))
	dec.UseNumber()
	if err := dec.Decode(&g); err != nil {
		t.Fatalf("decoding body %q: %v", got, err)
	}
	dec = json.NewDecoder(strings.NewReader(want))
	dec.UseNumber()
	if err := dec.Decode(&w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func stateValues(t *testing.T, v tftypes.Value) map[string]tftypes.Value {
	t.Helper()
	vals, err := objectValues(v)
	if err != nil {
		t.Fatal(err)
	}
	return vals
}

func TestResourceCreate(t *testing.T) {
	ctx := context.Background()
	c, reqs := fakeAPI(t, map[string]string{
		"POST /v6/widget":   `{"id":7}`,
		"GET /v6/widget(7)": `{"d":[` + widgetEntity + `]}`,
	})
	r := newTestResource(c)
	s := resourceSchema(testDescriptor)
	typ := s.Type().TerraformType(ctx)

	// Computed and optional-computed attributes are unknown in a create plan.
	plan := objectOf(t, typ, map[string]tftypes.Value{
		"name":              str("w"),
		"belongs_to__thing": num(42),
		"note":              tftypes.NewValue(tftypes.String, nil),
		"settings":          str(`{"a": [true], "b": 1}`),
		"secret":            str("hunter2"),
	}, true)
	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: plan}}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(typ, nil)}}
	r.Create(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics)
	}

	if len(*reqs) != 2 || (*reqs)[0].method != "POST" || (*reqs)[0].path != "/v6/widget" || (*reqs)[1].method != "GET" {
		t.Fatalf("requests = %+v", *reqs)
	}
	// Null and unknown values are not sent; a foreign key is a bare id; JSON is an object.
	assertBody(t, (*reqs)[0].body, `{"name":"w","belongs_to__thing":42,"settings":{"a":[true],"b":1},"secret":"hunter2"}`)

	got := stateValues(t, resp.State.Raw)
	want := map[string]tftypes.Value{
		"id":                num(7),
		"name":              str("w"),
		"belongs_to__thing": num(42),
		"note":              tftypes.NewValue(tftypes.String, nil),
		"is_enabled":        tftypes.NewValue(tftypes.Bool, true),
		"size":              num(9007199254740993),
		"settings":          str(`{"a": [true], "b": 1}`),
		"secret":            str("hunter2"),
	}
	for name, w := range want {
		if !got[name].Equal(w) {
			t.Errorf("state %s = %v, want %v", name, got[name], w)
		}
	}
}

func TestResourceRead(t *testing.T) {
	ctx := context.Background()
	s := resourceSchema(testDescriptor)
	typ := s.Type().TerraformType(ctx)
	prior := objectOf(t, typ, map[string]tftypes.Value{
		"id":       num(7),
		"name":     str("old"),
		"secret":   str("hunter2"),
		"settings": str(`{"a":[true],"b":1}`),
	}, false)

	t.Run("refreshes from the API", func(t *testing.T) {
		c, reqs := fakeAPI(t, map[string]string{"GET /v6/widget(7)": `{"d":[` + widgetEntity + `]}`})
		resp := resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: prior}}
		newTestResource(c).Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: prior}}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("read: %v", resp.Diagnostics)
		}
		if len(*reqs) != 1 {
			t.Errorf("sent %d requests, want 1", len(*reqs))
		}
		got := stateValues(t, resp.State.Raw)
		if !got["name"].Equal(str("w")) {
			t.Errorf("name = %v, want drift from the API", got["name"])
		}
		// The API returns a hash: the write-only value stays as written.
		if !got["secret"].Equal(str("hunter2")) {
			t.Errorf("secret = %v", got["secret"])
		}
		// Equal JSON keeps the configured formatting.
		if !got["settings"].Equal(str(`{"a":[true],"b":1}`)) {
			t.Errorf("settings = %v", got["settings"])
		}
	})

	for name, route := range map[string]map[string]string{
		"404":          {},
		"empty result": {"GET /v6/widget(7)": `{"d":[]}`},
	} {
		t.Run("removes on "+name, func(t *testing.T) {
			c, _ := fakeAPI(t, route)
			resp := resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: prior}}
			newTestResource(c).Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: prior}}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("read: %v", resp.Diagnostics)
			}
			if !resp.State.Raw.IsNull() {
				t.Errorf("state = %v, want removed", resp.State.Raw)
			}
		})
	}
}

func TestResourceUpdate(t *testing.T) {
	ctx := context.Background()
	s := resourceSchema(testDescriptor)
	typ := s.Type().TerraformType(ctx)
	state := objectOf(t, typ, map[string]tftypes.Value{
		"id":                num(7),
		"name":              str("w"),
		"belongs_to__thing": num(42),
		"note":              str("remove me"),
		"is_enabled":        tftypes.NewValue(tftypes.Bool, true),
		"size":              num(1),
		"secret":            str("hunter2"),
	}, false)
	// The plan changes name, removes note and leaves everything else.
	plan := objectOf(t, typ, map[string]tftypes.Value{
		"id":                num(7),
		"name":              str("renamed"),
		"belongs_to__thing": num(42),
		"note":              tftypes.NewValue(tftypes.String, nil),
		"is_enabled":        tftypes.NewValue(tftypes.Bool, true),
		"size":              tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
		"secret":            str("hunter2"),
	}, false)
	c, reqs := fakeAPI(t, map[string]string{
		"PATCH /v6/widget(7)": "OK",
		"GET /v6/widget(7)":   `{"d":[` + strings.Replace(widgetEntity, `"name":"w"`, `"name":"renamed"`, 1) + `]}`,
	})
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: state}}
	newTestResource(c).Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: s, Raw: plan},
		State: tfsdk.State{Schema: s, Raw: state},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	if (*reqs)[0].method != "PATCH" || (*reqs)[0].path != "/v6/widget(7)" {
		t.Fatalf("first request = %+v", (*reqs)[0])
	}
	// Only changed writable attributes are sent; a removed optional one is cleared.
	assertBody(t, (*reqs)[0].body, `{"name":"renamed","note":null}`)
	if got := stateValues(t, resp.State.Raw)["size"]; !got.Equal(num(9007199254740993)) {
		t.Errorf("size = %v, want the API value", got)
	}
}

func TestResourceUpdate_NoChangeSendsNoPatch(t *testing.T) {
	ctx := context.Background()
	s := resourceSchema(testDescriptor)
	typ := s.Type().TerraformType(ctx)
	state := objectOf(t, typ, map[string]tftypes.Value{"id": num(7), "name": str("w"), "belongs_to__thing": num(42)}, false)
	c, reqs := fakeAPI(t, map[string]string{"GET /v6/widget(7)": `{"d":[` + widgetEntity + `]}`})
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: state}}
	newTestResource(c).Update(ctx, resource.UpdateRequest{
		Plan: tfsdk.Plan{Schema: s, Raw: state}, State: tfsdk.State{Schema: s, Raw: state},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	for _, r := range *reqs {
		if r.method == "PATCH" {
			t.Errorf("sent a PATCH with nothing changed: %s", r.body)
		}
	}
}

func TestResourceDelete(t *testing.T) {
	ctx := context.Background()
	s := resourceSchema(testDescriptor)
	state := objectOf(t, s.Type().TerraformType(ctx), map[string]tftypes.Value{"id": num(7)}, false)
	for name, routes := range map[string]map[string]string{
		"deletes":         {"DELETE /v6/widget(7)": "OK"},
		"tolerates a 404": {},
	} {
		t.Run(name, func(t *testing.T) {
			c, reqs := fakeAPI(t, routes)
			resp := resource.DeleteResponse{State: tfsdk.State{Schema: s, Raw: state}}
			newTestResource(c).Delete(ctx, resource.DeleteRequest{State: tfsdk.State{Schema: s, Raw: state}}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("delete: %v", resp.Diagnostics)
			}
			if len(*reqs) != 1 || (*reqs)[0].method != "DELETE" || (*reqs)[0].path != "/v6/widget(7)" {
				t.Errorf("requests = %+v", *reqs)
			}
		})
	}
}

func TestResourceImportState(t *testing.T) {
	ctx := context.Background()
	s := resourceSchema(testDescriptor)
	typ := s.Type().TerraformType(ctx)
	resp := resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(typ, nil)}}
	newTestResource(nil).ImportState(ctx, resource.ImportStateRequest{ID: "7"}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("import: %v", resp.Diagnostics)
	}
	if got := stateValues(t, resp.State.Raw)["id"]; !got.Equal(num(7)) {
		t.Errorf("id = %v", got)
	}

	bad := resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(typ, nil)}}
	newTestResource(nil).ImportState(ctx, resource.ImportStateRequest{ID: "abc"}, &bad)
	if !bad.Diagnostics.HasError() {
		t.Error("import of a non-numeric id succeeded")
	}
}

func TestDataSourceRead(t *testing.T) {
	ctx := context.Background()
	s := dataSourceSchema(testDescriptor)
	typ := s.Type().TerraformType(ctx)
	if _, ok := s.Attributes["secret"]; ok {
		t.Error("data source exposes a write-only attribute")
	}

	read := func(t *testing.T, c *balena.Client, set map[string]tftypes.Value) datasource.ReadResponse {
		cfg := objectOf(t, typ, set, false)
		resp := datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(typ, nil)}}
		(&genericDataSource{d: testDescriptor, client: c}).Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: cfg}}, &resp)
		return resp
	}

	t.Run("by id", func(t *testing.T) {
		c, reqs := fakeAPI(t, map[string]string{"GET /v6/widget(7)": `{"d":[` + widgetEntity + `]}`})
		resp := read(t, c, map[string]tftypes.Value{"id": num(7), "name": str("ignored when id is set")})
		if resp.Diagnostics.HasError() {
			t.Fatalf("read: %v", resp.Diagnostics)
		}
		if len(*reqs) != 1 || (*reqs)[0].path != "/v6/widget(7)" {
			t.Errorf("requests = %+v", *reqs)
		}
		if got := stateValues(t, resp.State.Raw)["belongs_to__thing"]; !got.Equal(num(42)) {
			t.Errorf("belongs_to__thing = %v", got)
		}
	})

	t.Run("by filters", func(t *testing.T) {
		c, reqs := fakeAPI(t, map[string]string{"GET /v6/widget": `{"d":[` + widgetEntity + `]}`})
		resp := read(t, c, map[string]tftypes.Value{
			"name":              str("it's"),
			"belongs_to__thing": num(42),
			"is_enabled":        tftypes.NewValue(tftypes.Bool, true),
		})
		if resp.Diagnostics.HasError() {
			t.Fatalf("read: %v", resp.Diagnostics)
		}
		want := "$filter=" + strings.NewReplacer(" ", "+", "'", "%27", "$", "%24").
			Replace("name eq 'it''s' and belongs_to__thing eq 42 and is_enabled eq true") + "&$top=2"
		if len(*reqs) != 1 || (*reqs)[0].query != want {
			t.Errorf("query = %q, want %q", (*reqs)[0].query, want)
		}
	})

	for name, tc := range map[string]struct {
		routes map[string]string
		set    map[string]tftypes.Value
		want   string
	}{
		"no arguments": {nil, map[string]tftypes.Value{}, "set id or at least one other argument"},
		"no match":     {map[string]string{"GET /v6/widget": `{"d":[]}`}, map[string]tftypes.Value{"name": str("x")}, "no widget matches"},
		"ambiguous":    {map[string]string{"GET /v6/widget": `{"d":[` + widgetEntity + `,` + widgetEntity + `]}`}, map[string]tftypes.Value{"name": str("x")}, "more than one widget"},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := fakeAPI(t, tc.routes)
			resp := read(t, c, tc.set)
			if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), tc.want) {
				t.Errorf("diagnostics = %v, want an error containing %q", resp.Diagnostics, tc.want)
			}
		})
	}
}

func TestFromWire(t *testing.T) {
	fk := Attribute{Wire: "f", Type: TYPE_INT64, Codec: CODEC_FOREIGN_KEY}
	plain := Attribute{Wire: "n", Type: TYPE_INT64}
	for raw, want := range map[string]tftypes.Value{
		`{"__id": 5}`: num(5),
		`5`:           num(5),
		`null`:        tftypes.NewValue(tftypes.Number, nil),
	} {
		got, err := fk.fromWire(json.RawMessage(raw))
		if err != nil || !got.Equal(want) {
			t.Errorf("fk.fromWire(%s) = %v, %v; want %v", raw, got, err, want)
		}
	}
	if got, err := plain.fromWire(json.RawMessage(`"12"`)); err != nil || !got.Equal(num(12)) {
		t.Errorf("big integer string: %v, %v", got, err)
	}
	if got, err := plain.fromWire(nil); err != nil || !got.IsNull() {
		t.Errorf("missing field: %v, %v", got, err)
	}
	if _, err := plain.fromWire(json.RawMessage(`"x"`)); err == nil {
		t.Error("non-numeric integer accepted")
	}
	if _, err := (Attribute{Name: "j", Type: TYPE_STRING, Codec: CODEC_JSON}).toWire(str("{")); err == nil {
		t.Error("invalid JSON accepted")
	}
}
