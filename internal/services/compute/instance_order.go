package compute

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

// errAPITooOld ends the search for an ordered instance: an older API version serves no order_id on
// its instances, so nothing tells the ordered instance from the others.
var errAPITooOld = errors.New("OnetSolutions did not report which order each instance was created from, so " +
	"the provider cannot identify the instance it ordered and adopts none")

// orderedInstance returns the instance created from the order localOrderID among those listed
// with ?order_id=: only an instance whose own order_id equals it, since an API that ignores the
// filter lists every instance. It returns nil while none is listed, errAPITooOld when instances
// are listed and none carries the field, and an error when several match.
func orderedInstance(instances []client.Instance, localOrderID string) (*client.Instance, error) {
	var matches []client.Instance
	carried := false
	for i := range instances {
		field := instances[i].OrderID
		carried = carried || field.Present
		if field.Present && field.Value != nil && *field.Value == localOrderID {
			matches = append(matches, instances[i])
		}
	}
	switch {
	case len(matches) == 1:
		return &matches[0], nil
	case len(matches) > 1:
		ids := make([]string, 0, len(matches))
		for i := range matches {
			ids = append(ids, matches[i].ID)
		}
		slices.Sort(ids)
		return nil, fmt.Errorf("%d instances report order %s (%s), so the provider cannot tell which one it ordered",
			len(matches), localOrderID, strings.Join(ids, ", "))
	case len(instances) > 0 && !carried:
		return nil, errAPITooOld
	default:
		return nil, nil
	}
}

// orderRefused reports whether a failed order request was certainly not placed: the API
// answered a client error before checking out. A transport error, a client timeout, an HTTP
// 408 or a 5xx may come after the order was committed.
func orderRefused(err error) bool {
	apiErr, ok := client.AsAPIError(err)
	return ok && apiErr.StatusCode < http.StatusInternalServerError && apiErr.StatusCode != http.StatusRequestTimeout
}

// orderImportPrefix marks an import ID that names the order an instance was created from instead
// of the instance: "order:<local_order_id>", optionally after "<organization_id>/<project_id>/".
const orderImportPrefix = "order:"

func orderImportCommand(localOrderID string) string {
	return "`terraform import <address> " + orderImportPrefix + localOrderID + "`"
}

// orderReference names an order for a message: its local id, the one an import takes, and its
// number with the billing department, the one the console and the invoices show.
func orderReference(order *client.InstanceOrder) string {
	reference := "order " + order.OrderNumber
	if order.LocalOrderID != nil {
		reference += " (local order id " + *order.LocalOrderID + ")"
	}
	return reference
}

// awaitingPaymentDetail explains an order placed but unpaid: the API creates the instance only
// once the invoice is paid, so there is nothing to track yet. After the payment, an import by
// order finds the instance.
func awaitingPaymentDetail(order *client.InstanceOrder, name string) string {
	reference := orderReference(order)
	if order.InvoiceID != nil {
		reference += fmt.Sprintf(", invoice %d", *order.InvoiceID)
	}
	pay := "Pay its invoice in the console"
	if order.InvoicePaymentURL != nil && *order.InvoicePaymentURL != "" {
		pay += " (" + *order.InvoicePaymentURL + ")"
	}
	importStep := "import it with `terraform import` on this resource's address and the instance's id"
	if order.LocalOrderID != nil {
		importStep = "import it by its order with " + orderImportCommand(*order.LocalOrderID) + ", where <address> " +
			"is this resource's address (such as onetsolutions_instance.web)"
	}
	return fmt.Sprintf("The instance %q was ordered (%s), but the account balance did not cover it: %s is due. The "+
		"instance is created only once the order is paid, so nothing was added to the Terraform state.\n\n%s; once the "+
		"instance is created, %s. Do not apply this configuration again before importing it, or a second instance "+
		"is ordered.", name, reference, formatAmount(order.AmountDue), pay, importStep)
}

// cancellationReasonWord is a word as the API counts them in a cancellation reason.
var cancellationReasonWord = regexp.MustCompile(`[\p{L}\p{N}]+`)

func countWords(value string) int {
	return len(cancellationReasonWord.FindAllString(value, -1))
}

// cancellationDetail explains what DELETE did to an instance: a cancellation at the end of the
// billing period, not a termination.
func cancellationDetail(instance *client.Instance) string {
	if instance.Gone() {
		return fmt.Sprintf("Instance %s (%s) had already ended (status %s): nothing was requested.",
			instance.ID, instance.Name, instance.Status)
	}
	until := "the end of its billing period"
	if instance.BillingEndDate != nil && *instance.BillingEndDate != "" {
		until = *instance.BillingEndDate
	}
	return fmt.Sprintf("Instance %s (%s) was not deleted: its cancellation is requested, and it keeps running and "+
		"billing until %s, when it is terminated with its data. It is no longer in the Terraform state. To keep it, "+
		"revoke the cancellation in the console before that date, then import it again.", instance.ID, instance.Name, until)
}
