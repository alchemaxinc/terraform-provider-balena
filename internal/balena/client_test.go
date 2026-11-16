//go:build !integration

package balena

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(srv *httptest.Server, retries int) *Client {
	return NewClient(srv.URL, "test-token", "0.0.0-test", WithHTTPClient(srv.Client()), WithMaxRetries(retries))
}

func pineWrap(rows ...string) string {
	return `{"d":[` + strings.Join(rows, ",") + `]}`
}

func TestNewClient(t *testing.T) {
	c := NewClient("", "tok", "1.2.3")
	if c.baseURL != DEFAULT_BASE_URL || c.userAgent != "terraform-provider-balena/1.2.3" || c.maxRetries != DEFAULT_MAX_RETRIES {
		t.Errorf("defaults: %+v", c)
	}
	if c := NewClient("https://custom.example.com/", "tok", "0"); c.baseURL != "https://custom.example.com" {
		t.Errorf("baseURL = %q", c.baseURL)
	}
	custom := &http.Client{}
	if c := NewClient("", "tok", "0", WithHTTPClient(custom)); c.httpClient != custom {
		t.Error("custom HTTP client not used")
	}
}

func TestErrorHelpers(t *testing.T) {
	for _, tc := range []struct {
		err                         error
		notFound, conflict, unauthd bool
	}{
		{&APIError{StatusCode: 404}, true, false, false},
		{&APIError{StatusCode: 409}, false, true, false},
		{&APIError{StatusCode: 401}, false, false, true},
		{fmt.Errorf("wrapped: %w", &APIError{StatusCode: 404}), true, false, false},
		{io.EOF, false, false, false},
		{nil, false, false, false},
	} {
		if IsNotFound(tc.err) != tc.notFound || IsConflict(tc.err) != tc.conflict || IsUnauthorized(tc.err) != tc.unauthd {
			t.Errorf("%v: helpers disagree", tc.err)
		}
	}
	if got := (&APIError{StatusCode: 500, Body: "boom"}).Error(); got != "API returned status 500: boom" {
		t.Errorf("Error() = %q", got)
	}
}

// request records what the fake server received.
type request struct {
	Method, Path, RawQuery, Body string
	Header                       http.Header
}

// TestClientRequests drives every operation against a fake Pine server and checks the request it sends and how it
// reads the answer.
func TestClientRequests(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		response string
		call     func(c *Client) (any, error)
		want     request
		check    func(t *testing.T, got any, err error)
	}{
		{
			name:     "get by id selects and expands",
			response: pineWrap(`{"id":42,"app_name":"a","organization":{"__id":7},"is_for__device_type":[{"slug":"rpi"}]}`),
			call: func(c *Client) (any, error) {
				return c.Get(context.Background(), "v7", "application", 42, Query{
					Select: []string{"id", "app_name", "organization"},
					Expand: []Expand{{Field: "is_for__device_type", Select: []string{"slug"}}},
				})
			},
			want: request{Method: "GET", Path: "/v7/application(42)", RawQuery: "$select=id,app_name,organization&$expand=is_for__device_type($select=slug)"},
			check: func(t *testing.T, got any, err error) {
				row := got.(Row)
				if err != nil {
					t.Fatal(err)
				}
				if id, ok, _ := RefID(row["organization"]); !ok || id != 7 {
					t.Errorf("organization = %v", row["organization"])
				}
				if dt, ok, _ := Expanded(row["is_for__device_type"]); !ok || dt["slug"] != "rpi" {
					t.Errorf("is_for__device_type = %v", row["is_for__device_type"])
				}
			},
		},
		{
			name:     "get by id of a missing row is a 404",
			response: pineWrap(),
			call: func(c *Client) (any, error) {
				return c.Get(context.Background(), "resin", "image_profile", 9, Query{Select: []string{"id"}})
			},
			want: request{Method: "GET", Path: "/resin/image_profile(9)", RawQuery: "$select=id"},
			check: func(t *testing.T, _ any, err error) {
				if !IsNotFound(err) {
					t.Errorf("err = %v, want not found", err)
				}
			},
		},
		{
			name:     "list with a filter escapes quotes and keeps navigation",
			response: pineWrap(`{"id":1}`),
			call: func(c *Client) (any, error) {
				return c.List(context.Background(), "v7", "release", Query{
					Select: []string{"id"},
					Filter: And(Eq("commit", "it's"), "belongs_to__application/any(a:a/is_host eq true)", Eq("raw_version", "6.0.10+rev1")),
					Top:    2,
				})
			},
			want: request{Method: "GET", Path: "/v7/release",
				RawQuery: "$select=id&$filter=commit%20eq%20%27it%27%27s%27%20and%20belongs_to__application/any(a:a/is_host%20eq%20true)%20and%20raw_version%20eq%20%276.0.10%2Brev1%27&$top=2"},
			check: func(t *testing.T, got any, err error) {
				if err != nil || len(got.([]Row)) != 1 {
					t.Errorf("rows = %v, err = %v", got, err)
				}
			},
		},
		{
			name:     "create posts the body and reads the created row",
			status:   201,
			response: `{"id":5,"application":{"__id":3},"name":"A"}`,
			call: func(c *Client) (any, error) {
				return c.Create(context.Background(), "v7", "application_environment_variable", Row{"application": int64(3), "name": "A", "value": "b"})
			},
			want: request{Method: "POST", Path: "/v7/application_environment_variable", Body: `{"application":3,"name":"A","value":"b"}`},
			check: func(t *testing.T, got any, err error) {
				if id, err2 := CreatedID(got.(Row)); err != nil || err2 != nil || id != 5 {
					t.Errorf("id = %d, %v, %v", id, err, err2)
				}
			},
		},
		{
			name:   "patch sends only the given fields, null included",
			status: 200,
			call: func(c *Client) (any, error) {
				return nil, c.Patch(context.Background(), "v7", "device", 7, Row{"is_pinned_on__release": nil})
			},
			want: request{Method: "PATCH", Path: "/v7/device(7)", Body: `{"is_pinned_on__release":null}`},
		},
		{
			name:   "delete",
			status: 200,
			call: func(c *Client) (any, error) {
				return nil, c.Delete(context.Background(), "v7", "device_tag", 3)
			},
			want: request{Method: "DELETE", Path: "/v7/device_tag(3)"},
		},
		{
			name:     "401 is an error, never not found",
			status:   401,
			response: "Unauthorized",
			call: func(c *Client) (any, error) {
				return c.Get(context.Background(), "v7", "nonexistent", 1, Query{})
			},
			want: request{Method: "GET", Path: "/v7/nonexistent(1)"},
			check: func(t *testing.T, _ any, err error) {
				if !IsUnauthorized(err) || IsNotFound(err) {
					t.Errorf("err = %v", err)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got request
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				got = request{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery, Body: string(body), Header: r.Header}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()
			result, err := tc.call(newTestClient(srv, 0))
			if got.Method != tc.want.Method || got.Path != tc.want.Path || got.RawQuery != tc.want.RawQuery || got.Body != tc.want.Body {
				t.Errorf("request = %s %s?%s %s\nwant      %s %s?%s %s", got.Method, got.Path, got.RawQuery, got.Body,
					tc.want.Method, tc.want.Path, tc.want.RawQuery, tc.want.Body)
			}
			if got.Header.Get("Authorization") != "Bearer test-token" || got.Header.Get("User-Agent") != "terraform-provider-balena/0.0.0-test" {
				t.Errorf("headers = %v", got.Header)
			}
			if tc.check != nil {
				tc.check(t, result, err)
			} else if err != nil {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestRetries(t *testing.T) {
	tests := []struct {
		name      string
		statuses  []int
		retries   int
		wantCalls int32
		wantErr   bool
	}{
		{"429 with Retry-After", []int{429, 429, 200}, 5, 3, false},
		{"5xx", []int{502, 503, 200}, 5, 3, false},
		{"409", []int{409, 200}, 5, 2, false},
		{"gives up after max retries", []int{503, 503, 503, 503}, 2, 3, true},
		{"404 is not retried", []int{404, 200}, 5, 1, true},
		{"401 is not retried", []int{401, 200}, 5, 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := atomic.AddInt32(&calls, 1)
				status := tc.statuses[n-1]
				if status != 200 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(status)
					return
				}
				_, _ = w.Write([]byte(pineWrap(`{"id":1}`)))
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := newTestClient(srv, tc.retries).Get(ctx, "v7", "application", 1, Query{})
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v", err)
			}
			if got := atomic.LoadInt32(&calls); got != tc.wantCalls {
				t.Errorf("calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

func TestListPages(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		q := r.URL.RawQuery
		if !strings.Contains(q, "$orderby=id%20asc") || !strings.Contains(q, fmt.Sprintf("$top=%d", DEFAULT_PAGE_SIZE)) {
			t.Errorf("query = %s", q)
		}
		var rows []string
		switch {
		case strings.Contains(q, "$skip=0"):
			for i := 0; i < DEFAULT_PAGE_SIZE; i++ {
				rows = append(rows, fmt.Sprintf(`{"id":%d}`, i+1))
			}
		case strings.Contains(q, fmt.Sprintf("$skip=%d", DEFAULT_PAGE_SIZE)):
			rows = []string{`{"id":9001}`, `{"id":9002}`}
		default:
			t.Errorf("unexpected query %s", q)
		}
		_, _ = w.Write([]byte(pineWrap(rows...)))
	}))
	defer srv.Close()
	rows, err := newTestClient(srv, 0).List(context.Background(), "v7", "application", Query{Select: []string{"id"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != DEFAULT_PAGE_SIZE+2 || atomic.LoadInt32(&calls) != 2 {
		t.Errorf("rows = %d, calls = %d", len(rows), calls)
	}
}

func TestValueDecoding(t *testing.T) {
	var row Row
	dec := json.NewDecoder(strings.NewReader(`{"a":{"__id":12345678901},"b":null,"c":[],"d":"98765432109","e":[{"id":3}]}`))
	dec.UseNumber()
	if err := dec.Decode(&row); err != nil {
		t.Fatal(err)
	}
	if id, ok, err := RefID(row["a"]); !ok || err != nil || id != 12345678901 {
		t.Errorf("a = %d %v %v", id, ok, err)
	}
	if _, ok, err := RefID(row["b"]); ok || err != nil {
		t.Errorf("null reference should be absent")
	}
	if _, ok, err := RefID(row["c"]); ok || err != nil {
		t.Errorf("empty expansion should be absent")
	}
	if n, ok, err := ToInt64(row["d"]); !ok || err != nil || n != 98765432109 {
		t.Errorf("big integer string = %d %v %v", n, ok, err)
	}
	if id, ok, _ := RefID(row["e"]); !ok || id != 3 {
		t.Errorf("expanded reference = %d", id)
	}
	if _, _, err := RefID("nope"); err == nil {
		t.Errorf("expected an error for a malformed reference")
	}
}

func TestLiteral(t *testing.T) {
	for in, want := range map[any]string{
		"o'clock": "'o''clock'",
		int64(5):  "5",
		true:      "true",
		nil:       "null",
		1.5:       "1.5",
	} {
		if got := Literal(in); got != want {
			t.Errorf("Literal(%v) = %s, want %s", in, got, want)
		}
	}
	if got := EscapeFilter("a/b(c:d), e&f#g%h+i'j"); got != "a/b(c:d),%20e%26f%23g%25h%2Bi%27j" {
		t.Errorf("EscapeFilter = %s", got)
	}
}
