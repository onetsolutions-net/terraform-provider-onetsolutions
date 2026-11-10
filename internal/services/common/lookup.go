package common

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// FindOne returns the only item matching, for data sources that look an object up by a
// non-unique key such as its name. No match and several matches are errors; the latter lists
// the IDs so the user can switch to a lookup by id.
func FindOne[T any](items []T, match func(T) bool, idOf func(T) string, what, key string) (T, diag.Diagnostics) {
	var diags diag.Diagnostics
	item, found, ambiguous := MatchOne(items, match, idOf)
	switch {
	case len(ambiguous) > 0:
		diags.AddError(fmt.Sprintf("Several %ss found", what),
			fmt.Sprintf("%d objects of type %s match %s: %s. Look it up by id instead.", len(ambiguous), what, key, strings.Join(ambiguous, ", ")))
	case !found:
		diags.AddError(fmt.Sprintf("No %s found", what), fmt.Sprintf("No %s matches %s.", what, key))
	}
	return item, diags
}

// MatchOne returns the only item matching (found true). When several match, it returns the IDs
// of all of them in ambiguous instead, in the order of items.
func MatchOne[T any](items []T, match func(T) bool, idOf func(T) string) (item T, found bool, ambiguous []string) {
	var matches []T
	for _, candidate := range items {
		if match(candidate) {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 0:
		return item, false, nil
	case 1:
		return matches[0], true, nil
	default:
		for _, candidate := range matches {
			ambiguous = append(ambiguous, idOf(candidate))
		}
		return item, false, ambiguous
	}
}
