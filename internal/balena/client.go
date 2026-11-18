// Package balena is a thin, generic client for the Pine.js OData API of balena: rows are map[string]any, every table
// and API root is a parameter, and the Pine.js quirks live here once.
//
// Quirks handled:
//   - collections come wrapped in {"d": [...]}; a GET by id of a missing row answers 200 with an empty "d";
//   - references read as {"__id": N} (or as an expanded array) and are written as a bare integer;
//   - POST answers the created row without expanding navigation properties;
//   - 409, 429 and 5xx are retried with exponential backoff, honouring Retry-After;
//   - collections page with $top/$skip;
//   - 401 means forbidden or unknown (table or $select field) alike, so it is never read as "gone";
//   - a $filter that navigates relations must keep `/`, `(`, `)` and `:` unencoded.
package balena

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DEFAULT_BASE_URL is the default balena API endpoint (balena-cloud).
const DEFAULT_BASE_URL = "https://api.balena-cloud.com"

// DEFAULT_HTTP_TIMEOUT is the default per-request HTTP timeout.
const DEFAULT_HTTP_TIMEOUT = 60 * time.Second

// DEFAULT_MAX_RETRIES is the default number of retries for transient failures (409/429/5xx).
const DEFAULT_MAX_RETRIES = 5

// DEFAULT_PAGE_SIZE is the Pine.js page size used when listing collections.
const DEFAULT_PAGE_SIZE = 1000

// MAX_BACKOFF caps the exponential backoff between retries.
const MAX_BACKOFF = 30 * time.Second

// Row is one row of a Pine.js resource.
type Row = map[string]any

// APIError is a non-2xx response, or a synthesised 404 for an empty result.
type APIError struct {
	StatusCode int
	Body       string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("API returned status %d: %s", e.StatusCode, e.Body)
}

func statusOf(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

// IsNotFound reports whether the error is a 404 Not Found response.
func IsNotFound(err error) bool { return statusOf(err) == http.StatusNotFound }

// IsConflict reports whether the error is a 409 Conflict response.
func IsConflict(err error) bool { return statusOf(err) == http.StatusConflict }

// IsUnauthorized reports whether the error is a 401 response. Pine.js answers 401 both for a forbidden request and for
// an unknown table or field, so callers must not treat it as "not found".
func IsUnauthorized(err error) bool { return statusOf(err) == http.StatusUnauthorized }

// Client is the balena API client.
type Client struct {
	baseURL    string
	apiToken   string
	userAgent  string
	httpClient *http.Client
	maxRetries int
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client. The caller is responsible for its timeout.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// WithTimeout overrides the per-request HTTP timeout. Ignored when WithHTTPClient is used.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		if d > 0 {
			c.httpClient.Timeout = d
		}
	}
}

// WithMaxRetries overrides the number of retries for transient failures (409/429/5xx). Zero disables retries.
func WithMaxRetries(n int) ClientOption {
	return func(c *Client) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

// NewClient creates a client.
func NewClient(baseURL, apiToken, version string, opts ...ClientOption) *Client {
	if baseURL == "" {
		baseURL = DEFAULT_BASE_URL
	}
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiToken:   apiToken,
		userAgent:  "terraform-provider-balena/" + version,
		httpClient: &http.Client{Timeout: DEFAULT_HTTP_TIMEOUT},
		maxRetries: DEFAULT_MAX_RETRIES,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Expand is one $expand clause: a navigation field and the columns to read from the referenced row.
type Expand struct {
	Field  string
	Select []string
}

// Query describes a read.
type Query struct {
	Select []string
	Expand []Expand
	// Filter is an OData $filter expression; build it with Eq, And and Literal.
	Filter string
	// Top limits the result to one page of that size; zero pages through the whole collection.
	Top int
}

func (q Query) params() []string {
	var p []string
	if len(q.Select) > 0 {
		p = append(p, "$select="+strings.Join(q.Select, ","))
	}
	if len(q.Expand) > 0 {
		var parts []string
		for _, e := range q.Expand {
			part := e.Field
			if len(e.Select) > 0 {
				part += "($select=" + strings.Join(e.Select, ",") + ")"
			}
			parts = append(parts, part)
		}
		p = append(p, "$expand="+strings.Join(parts, ","))
	}
	if q.Filter != "" {
		p = append(p, "$filter="+EscapeFilter(q.Filter))
	}
	return p
}

// EscapeFilter percent-encodes a $filter expression for a query string. It encodes everything except unreserved
// characters and `/`, `(`, `)`, `:` and `,`, which the API rejects when encoded in navigation (lambda) filters.
func EscapeFilter(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9',
			ch == '-', ch == '.', ch == '_', ch == '~', ch == '/', ch == '(', ch == ')', ch == ':', ch == ',':
			b.WriteByte(ch)
		default:
			_, _ = fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

// Literal renders an OData literal: strings are quoted with embedded quotes doubled.
func Literal(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "'" + strings.ReplaceAll(x, "'", "''") + "'"
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return x.String()
	default:
		return "'" + strings.ReplaceAll(fmt.Sprint(x), "'", "''") + "'"
	}
}

// Eq renders `field eq value`.
func Eq(field string, value any) string {
	return field + " eq " + Literal(value)
}

// And joins expressions with `and`, skipping empty ones.
func And(exprs ...string) string {
	var parts []string
	for _, e := range exprs {
		if e != "" {
			parts = append(parts, e)
		}
	}
	return strings.Join(parts, " and ")
}

// RefID reads a reference value: {"__id": N}, an expanded [{"id": N}], or a bare number. ok is false for null or an
// empty expansion.
func RefID(v any) (int64, bool, error) {
	switch x := v.(type) {
	case nil:
		return 0, false, nil
	case map[string]any:
		return numberToInt64(x["__id"])
	case []any:
		if len(x) == 0 {
			return 0, false, nil
		}
		if row, ok := x[0].(map[string]any); ok {
			return numberToInt64(row["id"])
		}
	default:
		return numberToInt64(x)
	}
	return 0, false, fmt.Errorf("unexpected reference value %v", v)
}

// Expanded reads an expanded navigation value ([{...}] or []). ok is false for null or an empty expansion.
func Expanded(v any) (Row, bool, error) {
	switch x := v.(type) {
	case nil:
		return nil, false, nil
	case []any:
		if len(x) == 0 {
			return nil, false, nil
		}
		if row, ok := x[0].(map[string]any); ok {
			return row, true, nil
		}
	}
	return nil, false, fmt.Errorf("unexpected expansion %v", v)
}

func numberToInt64(v any) (int64, bool, error) {
	switch x := v.(type) {
	case nil:
		return 0, false, nil
	case json.Number:
		n, err := x.Int64()
		return n, err == nil, err
	case float64:
		return int64(x), true, nil
	case int64:
		return x, true, nil
	case int:
		return int64(x), true, nil
	case string:
		// Big Integer columns come back as strings.
		n, err := strconv.ParseInt(x, 10, 64)
		return n, err == nil, err
	}
	return 0, false, fmt.Errorf("unexpected number %v", v)
}

// ToInt64 converts a numeric row value.
func ToInt64(v any) (int64, bool, error) { return numberToInt64(v) }

func collectionPath(root, table string) string {
	return "/" + root + "/" + table
}

func rowPath(root, table string, id int64) string {
	return collectionPath(root, table) + "(" + strconv.FormatInt(id, 10) + ")"
}

func withParams(path string, params []string) string {
	if len(params) == 0 {
		return path
	}
	return path + "?" + strings.Join(params, "&")
}

func decodeCollection(data []byte) ([]Row, error) {
	var resp struct {
		D []Row `json:"d"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return resp.D, nil
}

// List reads the rows of a table that match the query, paging with $top/$skip unless q.Top is set.
func (c *Client) List(ctx context.Context, root, table string, q Query) ([]Row, error) {
	base := q.params()
	if q.Top > 0 {
		data, err := c.Do(ctx, http.MethodGet, withParams(collectionPath(root, table), append(base, "$top="+strconv.Itoa(q.Top))), nil)
		if err != nil {
			return nil, err
		}
		return decodeCollection(data)
	}
	var out []Row
	for skip := 0; ; skip += DEFAULT_PAGE_SIZE {
		params := append(append([]string{}, base...), "$orderby=id%20asc", fmt.Sprintf("$top=%d", DEFAULT_PAGE_SIZE), fmt.Sprintf("$skip=%d", skip))
		data, err := c.Do(ctx, http.MethodGet, withParams(collectionPath(root, table), params), nil)
		if err != nil {
			return nil, err
		}
		rows, err := decodeCollection(data)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
		if len(rows) < DEFAULT_PAGE_SIZE {
			return out, nil
		}
	}
}

// Get reads one row by id. Pine.js answers a missing id with an empty collection, which becomes a 404 APIError.
func (c *Client) Get(ctx context.Context, root, table string, id int64, q Query) (Row, error) {
	data, err := c.Do(ctx, http.MethodGet, withParams(rowPath(root, table, id), q.params()), nil)
	if err != nil {
		return nil, err
	}
	rows, err := decodeCollection(data)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, &APIError{StatusCode: http.StatusNotFound, Body: fmt.Sprintf("%s with id %d not found", table, id)}
	}
	return rows[0], nil
}

// Create posts a row and returns the created row as the API answers it (navigation properties unexpanded).
func (c *Client) Create(ctx context.Context, root, table string, body Row) (Row, error) {
	data, err := c.Do(ctx, http.MethodPost, collectionPath(root, table), body)
	if err != nil {
		return nil, err
	}
	var row Row
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&row); err != nil {
		return nil, fmt.Errorf("decoding create response: %w", err)
	}
	if d, ok := row["d"].([]any); ok && len(d) == 1 {
		if inner, ok := d[0].(map[string]any); ok {
			row = inner
		}
	}
	return row, nil
}

// CreatedID reads the id of a row returned by Create.
func CreatedID(row Row) (int64, error) {
	id, ok, err := numberToInt64(row["id"])
	if err != nil || !ok {
		return 0, fmt.Errorf("create response has no id: %v", row)
	}
	return id, nil
}

// Patch updates the given columns of a row.
func (c *Client) Patch(ctx context.Context, root, table string, id int64, body Row) error {
	_, err := c.Do(ctx, http.MethodPatch, rowPath(root, table, id), body)
	return err
}

// Delete removes a row.
func (c *Client) Delete(ctx context.Context, root, table string, id int64) error {
	_, err := c.Do(ctx, http.MethodDelete, rowPath(root, table, id), nil)
	return err
}

// Do sends one request to a path (with its query string), retrying 409/429/5xx with exponential backoff that honours
// Retry-After. It returns the body of a 2xx response and an *APIError otherwise.
func (c *Client) Do(ctx context.Context, method, rawPath string, body any) ([]byte, error) {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshalling request body: %w", err)
		}
	}
	u := c.baseURL + rawPath
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
		if err != nil {
			// The error quotes the URL, query included.
			return nil, fmt.Errorf("building %s %s: invalid request", method, withoutQuery(rawPath))
		}
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", c.userAgent)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("executing %s %s: %w", method, withoutQuery(rawPath), transportCause(err))
			if ctx.Err() != nil || attempt == c.maxRetries {
				return nil, lastErr
			}
			if werr := waitBeforeRetry(ctx, "", attempt); werr != nil {
				return nil, werr
			}
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("reading response body: %w", readErr)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return data, nil
		}
		apiErr := &APIError{StatusCode: resp.StatusCode, Body: string(data)}
		if !isRetryableStatus(resp.StatusCode) || attempt == c.maxRetries {
			return nil, apiErr
		}
		if werr := waitBeforeRetry(ctx, resp.Header.Get("Retry-After"), attempt); werr != nil {
			return nil, werr
		}
		lastErr = apiErr
	}
	return nil, lastErr
}

// withoutQuery drops the query string of a path: a $filter can hold values that must not reach diagnostics or logs,
// such as the value of a sensitive attribute a data source selects on.
func withoutQuery(rawPath string) string {
	if i := strings.IndexByte(rawPath, '?'); i >= 0 {
		return rawPath[:i]
	}
	return rawPath
}

// transportCause unwraps the *url.Error of a failed request, whose text repeats the full URL with its query string.
func transportCause(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// isRetryableStatus reports whether the given HTTP status code should trigger a retry.
func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusConflict,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// waitBeforeRetry sleeps before the next retry, honouring a Retry-After header (integer seconds or HTTP-date) and
// falling back to exponential backoff capped at MAX_BACKOFF.
func waitBeforeRetry(ctx context.Context, retryAfter string, attempt int) error {
	delay := time.Duration(1<<attempt) * time.Second
	if delay > MAX_BACKOFF {
		delay = MAX_BACKOFF
	}
	if retryAfter = strings.TrimSpace(retryAfter); retryAfter != "" {
		if secs, err := strconv.Atoi(retryAfter); err == nil && secs >= 0 {
			delay = time.Duration(secs) * time.Second
		} else if t, err := http.ParseTime(retryAfter); err == nil {
			if d := time.Until(t); d > 0 {
				delay = d
			}
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
