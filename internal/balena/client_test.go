//go:build !integration

package balena

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient creates a Client wired to the given httptest.Server.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	return NewClient(srv.URL, "test-token", "0.0.0-test", WithHTTPClient(srv.Client()))
}

// mustJSON marshals v to indented JSON for golden comparisons.
func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// pineWrap wraps items in the standard Pine.js OData response envelope.
func pineWrap[T any](items ...T) []byte {
	resp := struct {
		D []T `json:"d"`
	}{D: items}
	b, _ := json.Marshal(resp)
	return b
}

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient("", "tok", "1.2.3")

	if c.baseURL != DEFAULT_BASE_URL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, DEFAULT_BASE_URL)
	}
	if c.userAgent != "terraform-provider-balena/1.2.3" {
		t.Errorf("userAgent = %q", c.userAgent)
	}
	if c.apiToken != "tok" {
		t.Errorf("apiToken = %q", c.apiToken)
	}
}

func TestNewClient_CustomBaseURL(t *testing.T) {
	c := NewClient("https://custom.example.com", "tok", "0.1.0")

	if c.baseURL != "https://custom.example.com" {
		t.Errorf("baseURL = %q", c.baseURL)
	}
}

func TestNewClient_WithHTTPClient(t *testing.T) {
	custom := &http.Client{}
	c := NewClient("", "tok", "1.0.0", WithHTTPClient(custom))

	if c.httpClient != custom {
		t.Error("expected custom HTTP client to be used")
	}
}

func TestAPIError_Error(t *testing.T) {
	e := &APIError{StatusCode: 500, Body: "boom"}
	want := "API returned status 500: boom"
	if e.Error() != want {
		t.Errorf("Error() = %q, want %q", e.Error(), want)
	}
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"404 APIError", &APIError{StatusCode: 404, Body: "nope"}, true},
		{"500 APIError", &APIError{StatusCode: 500, Body: "err"}, false},
		{"nil error", nil, false},
		{"generic error", io.EOF, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNotFound(tc.err); got != tc.want {
				t.Errorf("IsNotFound() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Headers (User-Agent, Authorization)

func TestHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "terraform-provider-balena/0.0.0-test" {
			t.Errorf("User-Agent = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write(pineWrap[Application]())
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, _ = c.ListApplications(context.Background())
}

func TestNon2xxReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("access denied"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetApplication(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 403 {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if apiErr.Body != "access denied" {
		t.Errorf("Body = %q", apiErr.Body)
	}
}

func TestGetApplication_Success(t *testing.T) {
	app := Application{
		ID: 42, AppName: "my-app", Slug: "org/my-app", IsPublic: false,
		DeviceType: []DeviceTypeRef{{Slug: "raspberrypi4-64"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v6/application(42)") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.RawQuery, "expand") {
			t.Errorf("expected $expand in query, got %s", r.URL.RawQuery)
		}
		_, _ = w.Write(pineWrap(app))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetApplication(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 || got.AppName != "my-app" {
		t.Errorf("unexpected app: %+v", got)
	}
	if got.DeviceTypeSlug() != "raspberrypi4-64" {
		t.Errorf("DeviceTypeSlug() = %q", got.DeviceTypeSlug())
	}
}

func TestGetApplication_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Pine.js returns 200 with empty d array for missing IDs
		_, _ = w.Write(pineWrap[Application]())
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetApplication(context.Background(), 999)
	if !IsNotFound(err) {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestGetApplicationByName_Success(t *testing.T) {
	app := Application{ID: 10, AppName: "test-app"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "filter") {
			t.Errorf("expected OData filter in query, got %s", r.URL.RawQuery)
		}
		_, _ = w.Write(pineWrap(app))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetApplicationByName(context.Background(), "test-app")
	if err != nil {
		t.Fatal(err)
	}
	if got.AppName != "test-app" {
		t.Errorf("AppName = %q", got.AppName)
	}
}

func TestGetApplicationByName_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap[Application]())
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetApplicationByName(context.Background(), "no-such-app")
	if !IsNotFound(err) {
		t.Errorf("expected not-found, got %v", err)
	}
}

func TestGetApplicationByName_EscapesOData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.RawQuery
		// The name O'Reilly should be escaped as O''Reilly in the filter
		if !strings.Contains(query, "O%27%27Reilly") {
			t.Errorf("expected escaped OData in query, got %s", query)
		}
		_, _ = w.Write(pineWrap(Application{ID: 1, AppName: "O'Reilly"}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetApplicationByName(context.Background(), "O'Reilly")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 1 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestCreateApplication_Basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/v6/device_type") {
			_, _ = w.Write(pineWrap(DeviceType{ID: 676, Slug: "raspberrypi4-64", Name: "Raspberry Pi 4"}))
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/v6/application") {
			_, _ = w.Write(pineWrap(Application{ID: 100, AppName: "new-app"}))
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		if payload["app_name"] != "new-app" {
			t.Errorf("app_name = %v", payload["app_name"])
		}
		if payload["is_for__device_type"] != float64(676) {
			t.Errorf("is_for__device_type = %v, want 676", payload["is_for__device_type"])
		}
		if _, ok := payload["is_public"]; ok {
			t.Error("is_public should be omitted when nil")
		}
		_, _ = w.Write(mustJSON(t, applicationCreateResponse{ID: 100}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateApplication(context.Background(), "new-app", "raspberrypi4-64", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 100 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestCreateApplication_WithIsPublic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/v6/device_type") {
			_, _ = w.Write(pineWrap(DeviceType{ID: 676, Slug: "raspberrypi4-64", Name: "Raspberry Pi 4"}))
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/v6/application") {
			_, _ = w.Write(pineWrap(Application{ID: 101, AppName: "pub-app", IsPublic: true}))
			return
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		isPublic, ok := payload["is_public"]
		if !ok {
			t.Fatal("is_public should be present")
		}
		if isPublic != true {
			t.Errorf("is_public = %v", isPublic)
		}
		_, _ = w.Write(mustJSON(t, applicationCreateResponse{ID: 101}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	isPublic := true
	got, err := c.CreateApplication(context.Background(), "pub-app", "raspberrypi4-64", 1, &isPublic)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsPublic {
		t.Error("expected IsPublic to be true")
	}
}

func TestDeleteApplication_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.DeleteApplication(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteApplication_404Tolerance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.DeleteApplication(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !IsNotFound(err) {
		t.Errorf("expected IsNotFound=true, got %v", err)
	}
}

func TestGetApplicationEnvVar_Success(t *testing.T) {
	envVar := ApplicationEnvVar{ID: 5, App: ODataRef{ID: 1}, Name: "DB_HOST", Value: "localhost"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap(envVar))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetApplicationEnvVar(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "DB_HOST" || got.Value != "localhost" {
		t.Errorf("unexpected env var: %+v", got)
	}
}

func TestCreateApplicationEnvVar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		if payload["name"] != "MY_VAR" {
			t.Errorf("name = %v", payload["name"])
		}
		_, _ = w.Write(mustJSON(t, ApplicationEnvVar{ID: 20, Name: "MY_VAR", Value: "val"}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateApplicationEnvVar(context.Background(), 1, "MY_VAR", "val")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 20 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestUpdateApplicationEnvVar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "(10)") {
			t.Errorf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		if payload["value"] != "new-val" {
			t.Errorf("value = %v", payload["value"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.UpdateApplicationEnvVar(context.Background(), 10, "new-val"); err != nil {
		t.Fatal(err)
	}
}

func TestGetApplicationConfigVar_Success(t *testing.T) {
	cfgVar := ApplicationConfigVar{ID: 7, App: ODataRef{ID: 1}, Name: "RESIN_SUPERVISOR_DELTA", Value: "1"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap(cfgVar))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetApplicationConfigVar(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "RESIN_SUPERVISOR_DELTA" {
		t.Errorf("Name = %q", got.Name)
	}
}

func TestCreateApplicationConfigVar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		_, _ = w.Write(mustJSON(t, ApplicationConfigVar{ID: 30, Name: "CFG_KEY", Value: "cfgval"}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateApplicationConfigVar(context.Background(), 1, "CFG_KEY", "cfgval")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 30 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestGetDeviceEnvVar_Success(t *testing.T) {
	envVar := DeviceEnvVar{ID: 8, Device: ODataRef{ID: 55}, Name: "DEVICE_VAR", Value: "dv"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap(envVar))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetDeviceEnvVar(context.Background(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "DEVICE_VAR" {
		t.Errorf("Name = %q", got.Name)
	}
}

func TestCreateDeviceEnvVar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		if payload["device"] != float64(55) {
			t.Errorf("device = %v", payload["device"])
		}
		_, _ = w.Write(mustJSON(t, DeviceEnvVar{ID: 40, Name: "DEV_VAR", Value: "v1"}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateDeviceEnvVar(context.Background(), 55, "DEV_VAR", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 40 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestGetSSHKey_Success(t *testing.T) {
	key := SSHKey{ID: 3, Title: "my-key", PublicKey: "ssh-ed25519 AAAA..."}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v6/user__has__public_key(3)") {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write(pineWrap(key))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetSSHKey(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "my-key" {
		t.Errorf("Title = %q", got.Title)
	}
}

func TestCreateSSHKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		if payload["title"] != "deploy-key" {
			t.Errorf("title = %v", payload["title"])
		}
		_, _ = w.Write(mustJSON(t, SSHKey{ID: 50, Title: "deploy-key", PublicKey: "ssh-rsa AAAA..."}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateSSHKey(context.Background(), "deploy-key", "ssh-rsa AAAA...")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 50 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestGetApplicationTag_Success(t *testing.T) {
	tag := ApplicationTag{ID: 11, App: ODataRef{ID: 1}, TagKey: "env", Value: "prod"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap(tag))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetApplicationTag(context.Background(), 11)
	if err != nil {
		t.Fatal(err)
	}
	if got.TagKey != "env" || got.Value != "prod" {
		t.Errorf("unexpected tag: %+v", got)
	}
}

func TestCreateApplicationTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		if payload["tag_key"] != "region" {
			t.Errorf("tag_key = %v", payload["tag_key"])
		}
		_, _ = w.Write(mustJSON(t, ApplicationTag{ID: 60, TagKey: "region", Value: "us-east"}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateApplicationTag(context.Background(), 1, "region", "us-east")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 60 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestGetDeviceTag_Success(t *testing.T) {
	tag := DeviceTag{ID: 15, Device: ODataRef{ID: 99}, TagKey: "location", Value: "rack-3"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap(tag))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetDeviceTag(context.Background(), 15)
	if err != nil {
		t.Fatal(err)
	}
	if got.TagKey != "location" {
		t.Errorf("TagKey = %q", got.TagKey)
	}
}

func TestCreateDeviceTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		if payload["device"] != float64(99) {
			t.Errorf("device = %v", payload["device"])
		}
		_, _ = w.Write(mustJSON(t, DeviceTag{ID: 70, TagKey: "floor", Value: "2"}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateDeviceTag(context.Background(), 99, "floor", "2")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 70 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestGetServiceEnvVar_Success(t *testing.T) {
	envVar := ServiceEnvVar{ID: 80, Service: ODataRef{ID: 10}, Name: "SVC_VAR", Value: "sval"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap(envVar))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetServiceEnvVar(context.Background(), 80)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "SVC_VAR" || got.Value != "sval" {
		t.Errorf("unexpected env var: %+v", got)
	}
}

func TestCreateServiceEnvVar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		if payload["service"] != float64(10) {
			t.Errorf("service = %v", payload["service"])
		}
		if payload["name"] != "SVC_VAR" {
			t.Errorf("name = %v", payload["name"])
		}
		_, _ = w.Write(mustJSON(t, ServiceEnvVar{ID: 81, Name: "SVC_VAR", Value: "v1"}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateServiceEnvVar(context.Background(), 10, "SVC_VAR", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 81 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestGetDeviceConfigVar_Success(t *testing.T) {
	cfgVar := DeviceConfigVar{ID: 90, Device: ODataRef{ID: 55}, Name: "RESIN_HOST_CONFIG_gpu_mem", Value: "128"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap(cfgVar))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetDeviceConfigVar(context.Background(), 90)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "RESIN_HOST_CONFIG_gpu_mem" {
		t.Errorf("Name = %q", got.Name)
	}
}

func TestCreateDeviceConfigVar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		if payload["device"] != float64(55) {
			t.Errorf("device = %v", payload["device"])
		}
		_, _ = w.Write(mustJSON(t, DeviceConfigVar{ID: 91, Name: "CFG_KEY", Value: "cfgval"}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateDeviceConfigVar(context.Background(), 55, "CFG_KEY", "cfgval")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 91 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestGetReleaseTag_Success(t *testing.T) {
	tag := ReleaseTag{ID: 100, Release: ODataRef{ID: 42}, TagKey: "version", Value: "stable"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap(tag))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetReleaseTag(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if got.TagKey != "version" || got.Value != "stable" {
		t.Errorf("unexpected tag: %+v", got)
	}
}

func TestCreateReleaseTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		if payload["release"] != float64(42) {
			t.Errorf("release = %v", payload["release"])
		}
		if payload["tag_key"] != "channel" {
			t.Errorf("tag_key = %v", payload["tag_key"])
		}
		_, _ = w.Write(mustJSON(t, ReleaseTag{ID: 101, TagKey: "channel", Value: "beta"}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateReleaseTag(context.Background(), 42, "channel", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 101 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestGetImageEnvVar_Success(t *testing.T) {
	envVar := ImageEnvVar{ID: 110, ReleaseImage: ODataRef{ID: 77}, Name: "IMG_VAR", Value: "ival"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap(envVar))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetImageEnvVar(context.Background(), 110)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "IMG_VAR" || got.Value != "ival" {
		t.Errorf("unexpected env var: %+v", got)
	}
}

func TestCreateImageEnvVar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		if payload["release_image"] != float64(77) {
			t.Errorf("release_image = %v", payload["release_image"])
		}
		_, _ = w.Write(mustJSON(t, ImageEnvVar{ID: 111, Name: "IMG_VAR", Value: "v2"}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateImageEnvVar(context.Background(), 77, "IMG_VAR", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 111 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestGetServiceLabel_Success(t *testing.T) {
	label := ServiceLabel{ID: 120, Service: ODataRef{ID: 10}, LabelName: "io.balena.features.dbus", Value: "1"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap(label))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetServiceLabel(context.Background(), 120)
	if err != nil {
		t.Fatal(err)
	}
	if got.LabelName != "io.balena.features.dbus" || got.Value != "1" {
		t.Errorf("unexpected label: %+v", got)
	}
}

func TestCreateServiceLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		_ = json.Unmarshal(body, &payload)
		if payload["service"] != float64(10) {
			t.Errorf("service = %v", payload["service"])
		}
		if payload["label_name"] != "io.balena.features.supervisor-api" {
			t.Errorf("label_name = %v", payload["label_name"])
		}
		_, _ = w.Write(mustJSON(t, ServiceLabel{ID: 121, LabelName: "io.balena.features.supervisor-api", Value: "1"}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CreateServiceLabel(context.Background(), 10, "io.balena.features.supervisor-api", "1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 121 {
		t.Errorf("ID = %d", got.ID)
	}
}

// Service (data source)

func TestGetServiceByName_Success(t *testing.T) {
	svc := Service{ID: 200, App: ODataRef{ID: 42}, ServiceName: "main"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "filter") {
			t.Errorf("expected OData filter in query, got %s", r.URL.RawQuery)
		}
		_, _ = w.Write(pineWrap(svc))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetServiceByName(context.Background(), 42, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 200 || got.ServiceName != "main" {
		t.Errorf("unexpected service: %+v", got)
	}
}

func TestGetServiceByName_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap[Service]())
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetServiceByName(context.Background(), 42, "no-such-svc")
	if !IsNotFound(err) {
		t.Errorf("expected not-found, got %v", err)
	}
}

// Release (data source)

func TestGetRelease_Success(t *testing.T) {
	rel := Release{ID: 300, App: ODataRef{ID: 42}, Commit: "abc123", Status: "success"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap(rel))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetRelease(context.Background(), 300)
	if err != nil {
		t.Fatal(err)
	}
	if got.Commit != "abc123" || got.Status != "success" {
		t.Errorf("unexpected release: %+v", got)
	}
}

func TestGetReleaseByCommit_Success(t *testing.T) {
	rel := Release{ID: 301, App: ODataRef{ID: 42}, Commit: "def456", Status: "success"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "filter") {
			t.Errorf("expected OData filter in query, got %s", r.URL.RawQuery)
		}
		_, _ = w.Write(pineWrap(rel))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetReleaseByCommit(context.Background(), 42, "def456")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 301 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestGetReleaseByCommit_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap[Release]())
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetReleaseByCommit(context.Background(), 42, "nope")
	if !IsNotFound(err) {
		t.Errorf("expected not-found, got %v", err)
	}
}

// Organization (data source)

func TestGetOrganization_Success(t *testing.T) {
	org := Organization{ID: 400, Name: "My Org", Handle: "my-org"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap(org))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetOrganization(context.Background(), 400)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "My Org" || got.Handle != "my-org" {
		t.Errorf("unexpected org: %+v", got)
	}
}

func TestGetOrganizationByHandle_Success(t *testing.T) {
	org := Organization{ID: 401, Name: "Another Org", Handle: "another-org"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "filter") {
			t.Errorf("expected OData filter in query, got %s", r.URL.RawQuery)
		}
		_, _ = w.Write(pineWrap(org))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.GetOrganizationByHandle(context.Background(), "another-org")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 401 {
		t.Errorf("ID = %d", got.ID)
	}
}

func TestGetOrganizationByHandle_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pineWrap[Organization]())
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetOrganizationByHandle(context.Background(), "no-such-org")
	if !IsNotFound(err) {
		t.Errorf("expected not-found, got %v", err)
	}
}
