//go:build !integration

package balena

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The tests in this file pin the wire behavior of the parent/key/value child
// resources: method, path, query and canonical JSON body of every operation.
// They are the baseline that generated client code must reproduce unchanged.
// The paths and field names below are fixtures, deliberately written out
// rather than shared with client.go.

const (
	wireTestParentID int64 = 42
	wireTestID       int64 = 7
)

// childView is the type-independent projection of a child resource.
type childView struct {
	ID       int64
	ParentID int64
	Key      string
	Value    string
}

// childWireCase describes one child resource type and its expected wire names.
type childWireCase struct {
	name        string
	path        string
	parentField string
	keyField    string
	create      func(ctx context.Context, c *Client, parentID int64, key, value string) (childView, error)
	get         func(ctx context.Context, c *Client, id int64) (childView, error)
	update      func(ctx context.Context, c *Client, id int64, value string) error
	delete      func(ctx context.Context, c *Client, id int64) error
}

// childCase adapts the typed client methods of one child resource to childWireCase.
func childCase[T any](
	name, path, parentField, keyField string,
	create func(*Client, context.Context, int64, string, string) (*T, error),
	get func(*Client, context.Context, int64) (*T, error),
	update func(*Client, context.Context, int64, string) error,
	del func(*Client, context.Context, int64) error,
	view func(*T) childView,
) childWireCase {
	return childWireCase{
		name:        name,
		path:        path,
		parentField: parentField,
		keyField:    keyField,
		create: func(ctx context.Context, c *Client, parentID int64, key, value string) (childView, error) {
			r, err := create(c, ctx, parentID, key, value)
			if err != nil {
				return childView{}, err
			}
			return view(r), nil
		},
		get: func(ctx context.Context, c *Client, id int64) (childView, error) {
			r, err := get(c, ctx, id)
			if err != nil {
				return childView{}, err
			}
			return view(r), nil
		},
		update: func(ctx context.Context, c *Client, id int64, value string) error {
			return update(c, ctx, id, value)
		},
		delete: func(ctx context.Context, c *Client, id int64) error {
			return del(c, ctx, id)
		},
	}
}

var childWireCases = []childWireCase{
	childCase("ApplicationEnvVar", "/v6/application_environment_variable", "application", "name",
		(*Client).CreateApplicationEnvVar, (*Client).GetApplicationEnvVar,
		(*Client).UpdateApplicationEnvVar, (*Client).DeleteApplicationEnvVar,
		func(r *ApplicationEnvVar) childView { return childView{r.ID, r.App.ID, r.Name, r.Value} }),
	childCase("ApplicationConfigVar", "/v6/application_config_variable", "application", "name",
		(*Client).CreateApplicationConfigVar, (*Client).GetApplicationConfigVar,
		(*Client).UpdateApplicationConfigVar, (*Client).DeleteApplicationConfigVar,
		func(r *ApplicationConfigVar) childView { return childView{r.ID, r.App.ID, r.Name, r.Value} }),
	childCase("ServiceEnvVar", "/v6/service_environment_variable", "service", "name",
		(*Client).CreateServiceEnvVar, (*Client).GetServiceEnvVar,
		(*Client).UpdateServiceEnvVar, (*Client).DeleteServiceEnvVar,
		func(r *ServiceEnvVar) childView { return childView{r.ID, r.Service.ID, r.Name, r.Value} }),
	childCase("DeviceEnvVar", "/v6/device_environment_variable", "device", "name",
		(*Client).CreateDeviceEnvVar, (*Client).GetDeviceEnvVar,
		(*Client).UpdateDeviceEnvVar, (*Client).DeleteDeviceEnvVar,
		func(r *DeviceEnvVar) childView { return childView{r.ID, r.Device.ID, r.Name, r.Value} }),
	childCase("DeviceConfigVar", "/v6/device_config_variable", "device", "name",
		(*Client).CreateDeviceConfigVar, (*Client).GetDeviceConfigVar,
		(*Client).UpdateDeviceConfigVar, (*Client).DeleteDeviceConfigVar,
		func(r *DeviceConfigVar) childView { return childView{r.ID, r.Device.ID, r.Name, r.Value} }),
	childCase("DeviceServiceEnvVar", "/v6/device_service_environment_variable", "service_install", "name",
		(*Client).CreateDeviceServiceEnvVar, (*Client).GetDeviceServiceEnvVar,
		(*Client).UpdateDeviceServiceEnvVar, (*Client).DeleteDeviceServiceEnvVar,
		func(r *DeviceServiceEnvVar) childView { return childView{r.ID, r.ServiceInstall.ID, r.Name, r.Value} }),
	childCase("ImageEnvVar", "/v6/image_environment_variable", "release_image", "name",
		(*Client).CreateImageEnvVar, (*Client).GetImageEnvVar,
		(*Client).UpdateImageEnvVar, (*Client).DeleteImageEnvVar,
		func(r *ImageEnvVar) childView { return childView{r.ID, r.ReleaseImage.ID, r.Name, r.Value} }),
	childCase("ApplicationTag", "/v6/application_tag", "application", "tag_key",
		(*Client).CreateApplicationTag, (*Client).GetApplicationTag,
		(*Client).UpdateApplicationTag, (*Client).DeleteApplicationTag,
		func(r *ApplicationTag) childView { return childView{r.ID, r.App.ID, r.TagKey, r.Value} }),
	childCase("DeviceTag", "/v6/device_tag", "device", "tag_key",
		(*Client).CreateDeviceTag, (*Client).GetDeviceTag,
		(*Client).UpdateDeviceTag, (*Client).DeleteDeviceTag,
		func(r *DeviceTag) childView { return childView{r.ID, r.Device.ID, r.TagKey, r.Value} }),
	childCase("ReleaseTag", "/v6/release_tag", "release", "tag_key",
		(*Client).CreateReleaseTag, (*Client).GetReleaseTag,
		(*Client).UpdateReleaseTag, (*Client).DeleteReleaseTag,
		func(r *ReleaseTag) childView { return childView{r.ID, r.Release.ID, r.TagKey, r.Value} }),
	childCase("ServiceLabel", "/v6/service_label", "service", "label_name",
		(*Client).CreateServiceLabel, (*Client).GetServiceLabel,
		(*Client).UpdateServiceLabel, (*Client).DeleteServiceLabel,
		func(r *ServiceLabel) childView { return childView{r.ID, r.Service.ID, r.LabelName, r.Value} }),
}

// recordedRequest is what the fake API saw for one request.
type recordedRequest struct {
	method   string
	path     string
	rawQuery string
	body     []byte
}

// wireServer starts a fake API that records each request and answers every
// request with the given status and body.
func wireServer(t *testing.T, status int, respBody string) (*Client, *[]recordedRequest) {
	t.Helper()
	var reqs []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		reqs = append(reqs, recordedRequest{
			method:   r.Method,
			path:     r.URL.Path,
			rawQuery: r.URL.RawQuery,
			body:     body,
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "test-token", "0.0.0-test", WithHTTPClient(srv.Client()), WithMaxRetries(0))
	return c, &reqs
}

// onlyRequest asserts that exactly one request was sent, with the given
// method, path and raw query, and returns it.
func onlyRequest(t *testing.T, reqs []recordedRequest, method, path, rawQuery string) recordedRequest {
	t.Helper()
	if len(reqs) != 1 {
		t.Fatalf("sent %d requests, want 1: %+v", len(reqs), reqs)
	}
	r := reqs[0]
	if r.method != method {
		t.Errorf("method = %s, want %s", r.method, method)
	}
	if r.path != path {
		t.Errorf("path = %q, want %q", r.path, path)
	}
	if r.rawQuery != rawQuery {
		t.Errorf("query = %q, want %q", r.rawQuery, rawQuery)
	}
	return r
}

// assertJSONBody compares a request body with the expected JSON, ignoring key
// order and whitespace but not number versus string types.
func assertJSONBody(t *testing.T, got []byte, want string) {
	t.Helper()
	canonical := func(b []byte) interface{} {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		var v interface{}
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("decoding JSON %q: %v", b, err)
		}
		if dec.More() {
			t.Fatalf("trailing data after JSON value in %q", b)
		}
		return v
	}
	if g, w := canonical(got), canonical([]byte(want)); !reflect.DeepEqual(g, w) {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func assertEmptyBody(t *testing.T, got []byte) {
	t.Helper()
	if len(got) != 0 {
		t.Errorf("body = %q, want empty", got)
	}
}

// jsonObject marshals a map to a JSON object string.
func jsonObject(t *testing.T, m map[string]interface{}) string {
	t.Helper()
	return string(mustJSON(t, m))
}

func TestChildWire(t *testing.T) {
	ctx := context.Background()
	idPath := func(tc childWireCase) string { return tc.path + "(7)" }

	for _, tc := range childWireCases {
		t.Run(tc.name, func(t *testing.T) {
			// A read returns foreign keys as {"__id": N}.
			readItem := map[string]interface{}{
				"id":           wireTestID,
				tc.parentField: map[string]interface{}{"__id": wireTestParentID},
				tc.keyField:    "MY_KEY",
				"value":        "my value",
			}
			wantView := childView{ID: wireTestID, ParentID: wireTestParentID, Key: "MY_KEY", Value: "my value"}

			t.Run("Create", func(t *testing.T) {
				c, reqs := wireServer(t, http.StatusCreated, jsonObject(t, readItem))
				got, err := tc.create(ctx, c, wireTestParentID, "MY_KEY", "my value")
				if err != nil {
					t.Fatalf("create: %v", err)
				}
				r := onlyRequest(t, *reqs, http.MethodPost, tc.path, "")
				// A write sends the parent as a bare numeric ID.
				assertJSONBody(t, r.body, jsonObject(t, map[string]interface{}{
					tc.parentField: wireTestParentID,
					tc.keyField:    "MY_KEY",
					"value":        "my value",
				}))
				if got != wantView {
					t.Errorf("created = %+v, want %+v", got, wantView)
				}
			})

			t.Run("CreateEmptyValue", func(t *testing.T) {
				c, reqs := wireServer(t, http.StatusCreated, jsonObject(t, readItem))
				if _, err := tc.create(ctx, c, wireTestParentID, "MY_KEY", ""); err != nil {
					t.Fatalf("create: %v", err)
				}
				r := onlyRequest(t, *reqs, http.MethodPost, tc.path, "")
				assertJSONBody(t, r.body, jsonObject(t, map[string]interface{}{
					tc.parentField: wireTestParentID,
					tc.keyField:    "MY_KEY",
					"value":        "",
				}))
			})

			t.Run("CreateError", func(t *testing.T) {
				c, _ := wireServer(t, http.StatusBadRequest, `"Unique key constraint violated"`)
				_, err := tc.create(ctx, c, wireTestParentID, "MY_KEY", "my value")
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
					t.Fatalf("err = %v, want APIError 400", err)
				}
			})

			t.Run("Get", func(t *testing.T) {
				c, reqs := wireServer(t, http.StatusOK, `{"d":[`+jsonObject(t, readItem)+`]}`)
				got, err := tc.get(ctx, c, wireTestID)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				r := onlyRequest(t, *reqs, http.MethodGet, idPath(tc), "")
				assertEmptyBody(t, r.body)
				if got != wantView {
					t.Errorf("read = %+v, want %+v", got, wantView)
				}
			})

			t.Run("GetEmpty", func(t *testing.T) {
				c, reqs := wireServer(t, http.StatusOK, `{"d":[]}`)
				_, err := tc.get(ctx, c, wireTestID)
				if !IsNotFound(err) {
					t.Fatalf("err = %v, want not found", err)
				}
				onlyRequest(t, *reqs, http.MethodGet, idPath(tc), "")
			})

			t.Run("Get404", func(t *testing.T) {
				c, reqs := wireServer(t, http.StatusNotFound, `"Not Found"`)
				_, err := tc.get(ctx, c, wireTestID)
				if !IsNotFound(err) {
					t.Fatalf("err = %v, want not found", err)
				}
				onlyRequest(t, *reqs, http.MethodGet, idPath(tc), "")
			})

			t.Run("Update", func(t *testing.T) {
				c, reqs := wireServer(t, http.StatusOK, "OK")
				if err := tc.update(ctx, c, wireTestID, "new value"); err != nil {
					t.Fatalf("update: %v", err)
				}
				r := onlyRequest(t, *reqs, http.MethodPatch, idPath(tc), "")
				assertJSONBody(t, r.body, `{"value":"new value"}`)
			})

			t.Run("UpdateEmptyValue", func(t *testing.T) {
				c, reqs := wireServer(t, http.StatusOK, "OK")
				if err := tc.update(ctx, c, wireTestID, ""); err != nil {
					t.Fatalf("update: %v", err)
				}
				r := onlyRequest(t, *reqs, http.MethodPatch, idPath(tc), "")
				assertJSONBody(t, r.body, `{"value":""}`)
			})

			t.Run("Update404", func(t *testing.T) {
				c, _ := wireServer(t, http.StatusNotFound, `"Not Found"`)
				if err := tc.update(ctx, c, wireTestID, "new value"); !IsNotFound(err) {
					t.Fatalf("err = %v, want not found", err)
				}
			})

			t.Run("Delete", func(t *testing.T) {
				c, reqs := wireServer(t, http.StatusOK, "OK")
				if err := tc.delete(ctx, c, wireTestID); err != nil {
					t.Fatalf("delete: %v", err)
				}
				r := onlyRequest(t, *reqs, http.MethodDelete, idPath(tc), "")
				assertEmptyBody(t, r.body)
			})

			t.Run("Delete404", func(t *testing.T) {
				c, reqs := wireServer(t, http.StatusNotFound, `"Not Found"`)
				if err := tc.delete(ctx, c, wireTestID); !IsNotFound(err) {
					t.Fatalf("err = %v, want not found", err)
				}
				onlyRequest(t, *reqs, http.MethodDelete, idPath(tc), "")
			})
		})
	}
}

// TestChildWire_ListApplicationEnvVars pins the one child List operation,
// which the codegen pilot keeps handwritten.
func TestChildWire_ListApplicationEnvVars(t *testing.T) {
	item := `{"id":7,"application":{"__id":42},"name":"MY_KEY","value":"my value"}`
	c, reqs := wireServer(t, http.StatusOK, `{"d":[`+item+`]}`)
	got, err := c.ListApplicationEnvVars(context.Background(), wireTestParentID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	r := onlyRequest(t, *reqs, http.MethodGet, "/v6/application_environment_variable",
		"$filter=application+eq+42&$top=1000&$skip=0")
	assertEmptyBody(t, r.body)
	want := []ApplicationEnvVar{{ID: 7, App: ODataRef{ID: 42}, Name: "MY_KEY", Value: "my value"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list = %+v, want %+v", got, want)
	}
}

// TestChildWireCases_CoverEveryChildType guards against a child resource type
// being added to the client without a wire fixture. A child type is one with an
// Update<Name>(ctx, id int64, value string) error method.
func TestChildWireCases_CoverEveryChildType(t *testing.T) {
	updateSig := reflect.TypeOf((*Client).UpdateApplicationEnvVar)
	var fromClient []string
	ct := reflect.TypeOf(&Client{})
	for i := 0; i < ct.NumMethod(); i++ {
		m := ct.Method(i)
		if strings.HasPrefix(m.Name, "Update") && m.Type == updateSig {
			fromClient = append(fromClient, strings.TrimPrefix(m.Name, "Update"))
		}
	}
	var fromCases []string
	for _, tc := range childWireCases {
		fromCases = append(fromCases, tc.name)
	}
	sort.Strings(fromClient)
	sort.Strings(fromCases)
	if !reflect.DeepEqual(fromClient, fromCases) {
		t.Errorf("child types in client = %v, in childWireCases = %v", fromClient, fromCases)
	}
	if len(fromCases) != 11 {
		t.Errorf("childWireCases has %d entries, want 11", len(fromCases))
	}
}
