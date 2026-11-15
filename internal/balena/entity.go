package balena

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Entity is one Pine.js entity with its field values left JSON-encoded. The
// generic engine decodes the values against its own descriptors.
type Entity map[string]json.RawMessage

// ID decodes the entity's numeric id.
func (e Entity) ID() (int64, error) {
	raw, ok := e["id"]
	if !ok {
		return 0, fmt.Errorf("entity has no id")
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("decoding entity id: %w", err)
	}
	return strconv.ParseInt(n.String(), 10, 64)
}

// GetEntity retrieves one entity of the collection at path by id.
func (c *Client) GetEntity(ctx context.Context, path string, id int64) (Entity, error) {
	e, err := doGetByID[Entity](ctx, c, path, id)
	if err != nil {
		return nil, err
	}
	return *e, nil
}

// FindEntities returns at most top entities of the collection at path that
// match the OData filter expression.
func (c *Client) FindEntities(ctx context.Context, path, filter string, top int) ([]Entity, error) {
	query := fmt.Sprintf("$filter=%s&$top=%d", url.QueryEscape(filter), top)
	data, err := c.do(ctx, http.MethodGet, appendQuery(path, query), nil)
	if err != nil {
		return nil, err
	}
	var resp pineResponse[Entity]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return resp.D, nil
}

// CreateEntity POSTs body to the collection at path and returns the created entity.
func (c *Client) CreateEntity(ctx context.Context, path string, body map[string]any) (Entity, error) {
	e, err := doCreate[Entity](ctx, c, path, body)
	if err != nil {
		return nil, err
	}
	return *e, nil
}

// PatchEntity PATCHes body onto the entity of the collection at path.
func (c *Client) PatchEntity(ctx context.Context, path string, id int64, body map[string]any) error {
	return doPatch(ctx, c, path, id, body)
}

// DeleteEntity deletes the entity of the collection at path.
func (c *Client) DeleteEntity(ctx context.Context, path string, id int64) error {
	return doDelete(ctx, c, path, id)
}

// SelectFields asks the collection at path for the named fields without
// returning any rows. The API rejects a field name it does not serve, so this
// verifies field names without reading data.
func (c *Client) SelectFields(ctx context.Context, path string, fields []string) error {
	_, err := c.do(ctx, http.MethodGet, appendQuery(path, "$select="+strings.Join(fields, ",")+"&$top=0"), nil)
	return err
}
