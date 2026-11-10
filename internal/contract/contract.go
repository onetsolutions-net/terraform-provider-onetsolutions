// Package contract tells whether an operation belongs to the public API contract: the OpenAPI
// snapshot in schema/openapi.json plus a documented allow-list of endpoints the API serves but
// does not publish yet.
package contract

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/schema"
)

// Operation is one HTTP method on one OpenAPI path template.
type Operation struct {
	Method string
	Path   string
}

func (o Operation) String() string {
	return o.Method + " " + o.Path
}

// Unpublished lists the operations the provider calls although the public schema does not
// publish them. Each entry is an operation the API serves without publishing it yet. It is empty:
// every operation the provider calls is published.
var Unpublished []Operation

var (
	loadOnce   sync.Once
	operations map[Operation]struct{}
	errLoad    error
)

// Published returns every operation of the OpenAPI snapshot.
func Published() (map[Operation]struct{}, error) {
	loadOnce.Do(func() {
		operations, errLoad = parse(schema.OpenAPI)
	})
	return operations, errLoad
}

// Check returns an error unless the operation is published or allow-listed.
func Check(method, path string) error {
	published, err := Published()
	if err != nil {
		return err
	}
	return check(Operation{Method: strings.ToUpper(method), Path: path}, published, Unpublished)
}

func check(op Operation, published map[Operation]struct{}, allowList []Operation) error {
	if _, ok := published[op]; ok || slices.Contains(allowList, op) {
		return nil
	}
	return fmt.Errorf("%s is not an operation of schema/openapi.json nor of the documented allow-list", op)
}

// SortedPublished returns the published operations in a stable order, for messages and tests.
func SortedPublished() ([]Operation, error) {
	published, err := Published()
	if err != nil {
		return nil, err
	}
	ops := make([]Operation, 0, len(published))
	for op := range published {
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].String() < ops[j].String() })
	return ops, nil
}

func parse(document []byte) (map[Operation]struct{}, error) {
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(document, &spec); err != nil {
		return nil, fmt.Errorf("parsing schema/openapi.json: %w", err)
	}
	methods := map[string]struct{}{
		http.MethodGet: {}, http.MethodPost: {}, http.MethodPut: {}, http.MethodPatch: {}, http.MethodDelete: {},
	}
	ops := make(map[Operation]struct{})
	for path, item := range spec.Paths {
		for method := range item {
			upper := strings.ToUpper(method)
			if _, ok := methods[upper]; ok {
				ops[Operation{Method: upper, Path: path}] = struct{}{}
			}
		}
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("schema/openapi.json declares no operation")
	}
	return ops, nil
}
