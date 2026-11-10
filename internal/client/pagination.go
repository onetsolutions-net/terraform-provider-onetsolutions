package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// MaxPageSize is the largest page the API serves.
const MaxPageSize = 100

const maxPages = 10000

type pageEnvelope[T any] struct {
	Count      int     `json:"count"`
	Next       *string `json:"next"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
	TotalPages int     `json:"total_pages"`
	Results    []T     `json:"results"`
}

// listAll reads every page of a list route. It accepts the paginated envelope
// {count, next, previous, page, page_size, total_pages, results} and a bare JSON array, which
// some list endpoints return.
func listAll[T any](ctx context.Context, c *Client, r Route, params Params, query url.Values) ([]T, error) {
	all := []T{}
	for page := 1; page <= maxPages; page++ {
		items, last, err := listPage[T](ctx, c, r, params, query, page)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if last {
			return all, nil
		}
	}
	return nil, fmt.Errorf("%s %s: more than %d pages", r.method, r.template, maxPages)
}

// listFirstPage reads only the first page of a list route (at most MaxPageSize items), for the
// lists the API orders newest first when only the recent items matter.
func listFirstPage[T any](ctx context.Context, c *Client, r Route, params Params) ([]T, error) {
	items, _, err := listPage[T](ctx, c, r, params, nil, 1)
	return items, err
}

// listPage reads one page and reports whether it is the last one.
func listPage[T any](ctx context.Context, c *Client, r Route, params Params, query url.Values, page int) (items []T, last bool, err error) {
	q := url.Values{}
	for key, values := range query {
		q[key] = append([]string(nil), values...)
	}
	q.Set("page", strconv.Itoa(page))
	q.Set("page_size", strconv.Itoa(MaxPageSize))

	raw, err := c.doRaw(ctx, r, params, q, nil)
	if err != nil {
		return nil, false, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return []T{}, true, nil
	}
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, false, fmt.Errorf("%s %s: decoding the list: %w", r.method, r.template, err)
		}
		return items, true, nil
	}

	var envelope pageEnvelope[T]
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, false, fmt.Errorf("%s %s: decoding page %d: %w", r.method, r.template, page, err)
	}
	last = envelope.Next == nil || len(envelope.Results) == 0 || (envelope.TotalPages > 0 && page >= envelope.TotalPages)
	return envelope.Results, last, nil
}
