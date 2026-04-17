//go:build !integration

package balena

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newFastRetryClient builds a client pointed at srv with near-zero backoff
// by overriding maxRetries and using the test server's HTTP client.
func newFastRetryClient(t *testing.T, srv *httptest.Server, maxRetries int) *Client {
	t.Helper()
	c := NewClient(srv.URL, "tok", "0.0.0-test",
		WithHTTPClient(srv.Client()),
		WithMaxRetries(maxRetries),
	)
	return c
}

func TestDo_RetriesOn429WithRetryAfter(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(pineWrap(Application{ID: 1, AppName: "ok"}))
	}))
	defer srv.Close()

	c := newFastRetryClient(t, srv, 5)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.GetApplication(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestDo_RetriesOn5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if n == 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(pineWrap(Application{ID: 1}))
	}))
	defer srv.Close()

	c := newFastRetryClient(t, srv, 5)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.GetApplication(ctx, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestDo_GivesUpAfterMaxRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newFastRetryClient(t, srv, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.GetApplication(ctx, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3 (1 initial + 2 retries)", got)
	}
}

func TestDo_DoesNotRetryOn404(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newFastRetryClient(t, srv, 5)
	_, err := c.GetApplication(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestDoListAll_Paginates(t *testing.T) {
	// First page: DEFAULT_PAGE_SIZE items, second page: 3 items (short -> stop).
	page1 := make([]Application, DEFAULT_PAGE_SIZE)
	for i := range page1 {
		page1[i] = Application{ID: int64(i + 1)}
	}
	page2 := []Application{{ID: 9001}, {ID: 9002}, {ID: 9003}}

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		q := r.URL.RawQuery
		switch {
		case strings.Contains(q, "$skip=0"):
			_, _ = w.Write(pineWrap(page1...))
		case strings.Contains(q, fmt.Sprintf("$skip=%d", DEFAULT_PAGE_SIZE)):
			_, _ = w.Write(pineWrap(page2...))
		default:
			t.Errorf("unexpected query: %s", q)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := newFastRetryClient(t, srv, 0)
	got, err := c.ListApplications(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != DEFAULT_PAGE_SIZE+3 {
		t.Errorf("len(got) = %d, want %d", len(got), DEFAULT_PAGE_SIZE+3)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("calls = %d, want 2", atomic.LoadInt32(&calls))
	}
}

func TestDoListAll_SinglePage(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write(pineWrap(Application{ID: 1}, Application{ID: 2}))
	}))
	defer srv.Close()

	c := newFastRetryClient(t, srv, 0)
	got, err := c.ListApplications(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len(got) = %d, want 2", len(got))
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("calls = %d, want 1", atomic.LoadInt32(&calls))
	}
}
