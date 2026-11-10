package compute

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// uniqueIgnoringCase rejects a set holding two strings that differ only by case: the API keeps
// the first and drops the other, so the stored tags would differ from the configuration.
type uniqueIgnoringCase struct{}

func (uniqueIgnoringCase) Description(context.Context) string {
	return "values must be unique, ignoring case"
}

func (v uniqueIgnoringCase) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (uniqueIgnoringCase) ValidateSet(_ context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	seen := map[string]string{}
	for _, element := range req.ConfigValue.Elements() {
		value, ok := element.(types.String)
		if !ok || value.IsNull() || value.IsUnknown() {
			continue
		}
		key := strings.ToLower(value.ValueString())
		if previous, duplicate := seen[key]; duplicate {
			resp.Diagnostics.AddAttributeError(req.Path, "Duplicate tag",
				fmt.Sprintf("%q and %q differ only by case: the API keeps one of them. Keep one.", previous, value.ValueString()))
			return
		}
		seen[key] = value.ValueString()
	}
}

// minWords rejects a text of fewer words than the API's cancellation reason requires, counted as
// the API counts them (runs of letters and digits).
type minWords int

func (m minWords) Description(context.Context) string {
	return fmt.Sprintf("value must have at least %d words", int(m))
}

func (m minWords) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

func (m minWords) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if n := countWords(req.ConfigValue.ValueString()); n < int(m) {
		resp.Diagnostics.AddAttributeError(req.Path, "Reason too short",
			fmt.Sprintf("The reason has %d words; the API requires at least %d. Explain the cancellation in a sentence.", n, int(m)))
	}
}
