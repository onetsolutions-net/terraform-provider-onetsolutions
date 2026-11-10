package network

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

// Name prefixes of the rules One Click Apps installs and keeps up to date on an instance
// through the API: the current one and the legacy one.
const (
	managedRulePrefix       = "One Click Apps · "
	legacyManagedRulePrefix = "one-click:"
)

var managedRulePrefixes = []string{managedRulePrefix, legacyManagedRulePrefix}

// isPlatformManaged reports whether a rule belongs to the platform rather than the customer: a
// rule whose source is a well-known platform source (source_label set, e.g. the control-plane
// egress) or whose name carries a One Click Apps prefix. The provider never changes, deletes,
// reads into state or reports drift on such a rule.
func isPlatformManaged(rule *client.FirewallRule) bool {
	return rule.SourceLabel != nil || hasManagedPrefix(rule.Name)
}

func hasManagedPrefix(name string) bool {
	for _, prefix := range managedRulePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func customerRules(rules []client.FirewallRule) []client.FirewallRule {
	out := make([]client.FirewallRule, 0, len(rules))
	for i := range rules {
		if !isPlatformManaged(&rules[i]) {
			out = append(out, rules[i])
		}
	}
	return out
}

// unmanagedName rejects a rule name that would make the rule look platform-managed.
type unmanagedName struct{}

func (unmanagedName) Description(context.Context) string {
	return fmt.Sprintf("value must not start with %q or %q, which name the rules the platform manages",
		managedRulePrefix, legacyManagedRulePrefix)
}

func (v unmanagedName) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (unmanagedName) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if name := req.ConfigValue.ValueString(); hasManagedPrefix(name) {
		resp.Diagnostics.AddAttributeError(req.Path, "Reserved rule name",
			fmt.Sprintf("%q starts with a prefix reserved for the rules the platform manages (%q, %q). "+
				"Those rules are kept by the platform and never managed by Terraform; choose another name.",
				name, managedRulePrefix, legacyManagedRulePrefix))
	}
}
