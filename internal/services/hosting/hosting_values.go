package hosting

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

// Billing cycles of a web hosting, shortest first. free and one_time exist only on hostings set
// up that way, such as imported ones: a plan is ordered on a recurring cycle.
var (
	webHostingOrderableCycles = []string{"monthly", "quarterly", "semi_annually", "annually", "biennially", "triennially"}
	webHostingBillingCycles   = append([]string{"free", "one_time"}, webHostingOrderableCycles...)
)

const webHostingDefaultCycle = "monthly"

// webHostingCycleRank orders the recurring cycles; 0 for a cycle that is not one.
func webHostingCycleRank(cycle string) int {
	return slices.Index(webHostingOrderableCycles, cycle) + 1
}

// Domain sources of an order.
const (
	sourceRegister = "register"
	sourceTransfer = "transfer"
	sourceExisting = "existing"
	sourceExternal = "external"
)

var webHostingDomainSources = []string{sourceRegister, sourceTransfer, sourceExisting, sourceExternal}

// webHostingDomainPattern is a lowercase domain name without a trailing dot, as the API stores it.
var webHostingDomainPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

const webHostingDomainMessage = "must be a lowercase domain name without a trailing dot, such as example.com"

// cancellationWord is a word as the API counts them in a cancellation reason.
var cancellationWord = regexp.MustCompile(`[\p{L}\p{N}]+`)

func cancellationWords(value string) int {
	return len(cancellationWord.FindAllString(value, -1))
}

// minReasonWords rejects a text of fewer words than the API's cancellation reason requires.
type minReasonWords int

func (m minReasonWords) Description(context.Context) string {
	return fmt.Sprintf("value must have at least %d words", int(m))
}

func (m minReasonWords) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

func (m minReasonWords) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if n := cancellationWords(req.ConfigValue.ValueString()); n < int(m) {
		resp.Diagnostics.AddAttributeError(req.Path, "Reason too short",
			fmt.Sprintf("The reason has %d words; the API requires at least %d. Explain the cancellation in a sentence.", n, int(m)))
	}
}

func nullableString(value *string) types.String {
	if value == nil || *value == "" {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

func formatEUR(value *float64) string {
	if value == nil {
		return "an unknown amount"
	}
	return fmt.Sprintf("%.2f", *value)
}

// hostingPlanPrice returns the price of a plan on a billing cycle, false when it is not sold on it.
func hostingPlanPrice(plan *client.HostingPlan, cycle string) (string, bool) {
	for _, price := range plan.Pricing {
		if price.BillingCycle == cycle {
			return price.Price, true
		}
	}
	return "", false
}

func hostingPlanCycles(plan *client.HostingPlan) string {
	cycles := make([]string, 0, len(plan.Pricing))
	for _, price := range plan.Pricing {
		cycles = append(cycles, price.BillingCycle)
	}
	if len(cycles) == 0 {
		return "none"
	}
	return strings.Join(cycles, ", ")
}

func hostingPlanNames(plans []client.HostingPlan) string {
	names := make([]string, 0, len(plans))
	for i := range plans {
		names = append(names, fmt.Sprintf("%s (%s)", plans[i].ID, plans[i].Slug))
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// webHostingGone reports whether a web hosting is gone for its owner: not found, terminated or
// cancelled. What it carries can then no longer be read or changed, only forgotten.
func webHostingGone(ctx context.Context, c *client.Client, scope client.Scope, id string) (bool, error) {
	hosting, err := c.GetWebHosting(ctx, scope, id)
	if client.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return hosting.Gone(), nil
}

// goneWithWebHosting reports whether a failed change to a hosting's object means the object is
// gone with its hosting: a not found, or a refusal because the hosting has ended.
func goneWithWebHosting(ctx context.Context, c *client.Client, scope client.Scope, hostingID string, err error) bool {
	if client.IsNotFound(err) {
		return true
	}
	if !client.IsForbidden(err) {
		return false
	}
	gone, readErr := webHostingGone(ctx, c, scope, hostingID)
	return readErr == nil && gone
}
