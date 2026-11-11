package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// TLD is the TLD schema: an extension on sale, with its prices per registration length.
type TLD struct {
	ID                      string       `json:"id"`
	TLD                     string       `json:"tld"`
	DisplayName             string       `json:"display_name"`
	Category                string       `json:"category"`
	IsActive                bool         `json:"is_active"`
	IsRegistrationAvailable bool         `json:"is_registration_available"`
	IsTransferAvailable     bool         `json:"is_transfer_available"`
	WhoisPrivacyAvailable   bool         `json:"whois_privacy_available"`
	WhoisPrivacyPrice       *string      `json:"whois_privacy_price"`
	TransferLockAvailable   bool         `json:"transfer_lock_available"`
	MinRegistrationYears    int64        `json:"min_registration_years"`
	MaxRegistrationYears    int64        `json:"max_registration_years"`
	Pricing                 []TLDPricing `json:"pricing"`
}

// TLDPricing is the TLDPricing schema: the prices of one registration length.
type TLDPricing struct {
	Years             int64  `json:"years"`
	RegistrationPrice string `json:"registration_price"`
	RenewalPrice      string `json:"renewal_price"`
	TransferPrice     string `json:"transfer_price"`
}

// TLDFilter narrows ListTLDs; empty fields do not filter.
type TLDFilter struct {
	TLD                     string
	Category                string
	IsRegistrationAvailable *bool
}

// DomainSuggestion is the DomainSuggestion schema.
type DomainSuggestion struct {
	DomainName string  `json:"domain_name"`
	TLD        string  `json:"tld"`
	Available  bool    `json:"available"`
	Price      *string `json:"price"`
}

// DomainAvailability is the DomainAvailabilityResponse schema. Price is the first year's
// registration price, null when the name is not available.
type DomainAvailability struct {
	DomainName  string             `json:"domain_name"`
	Available   bool               `json:"available"`
	TLD         string             `json:"tld"`
	Price       *string            `json:"price"`
	Suggestions []DomainSuggestion `json:"suggestions"`
}

// DomainOrder is the DomainOrderResponse schema. The domain exists from the answer on, pending
// until the order is paid and the registration completes.
type DomainOrder struct {
	OrderID           *int64   `json:"order_id"`
	OrderNumber       *string  `json:"order_number"`
	InvoiceID         *int64   `json:"invoice_id"`
	LocalOrderID      *string  `json:"local_order_id"`
	Total             *float64 `json:"total"`
	AmountDue         *float64 `json:"amount_due"`
	CreditApplied     *float64 `json:"credit_applied"`
	Status            string   `json:"status"`
	InvoicePaymentURL *string  `json:"invoice_payment_url"`
	DomainID          string   `json:"domain_id"`
	DomainStatus      string   `json:"domain_status"`
}

// AwaitsPayment reports whether the account balance did not cover the order: the domain is
// registered only once its invoice is paid.
func (o *DomainOrder) AwaitsPayment() bool {
	return o.AmountDue != nil && *o.AmountDue > 0
}

// DomainOrderRegister is the order_type of a new registration.
const DomainOrderRegister = "register"

var (
	routeListTLDs                = newRoute(http.MethodGet, "/v1/domains/tlds")
	routeCheckDomainAvailability = newRoute(http.MethodGet, "/v1/domains/availability/check")
	routeCreateDomainOrder       = newRoute(http.MethodPost, domainsPath+"/order")
)

// ListTLDs lists the extensions on sale.
func (c *Client) ListTLDs(ctx context.Context, filter TLDFilter) ([]TLD, error) {
	query := url.Values{}
	if filter.TLD != "" {
		query.Set("tld", filter.TLD)
	}
	if filter.Category != "" {
		query.Set("category", filter.Category)
	}
	if filter.IsRegistrationAvailable != nil {
		query.Set("is_registration_available", strconv.FormatBool(*filter.IsRegistrationAvailable))
	}
	return listAll[TLD](ctx, c, routeListTLDs, Params{}, query)
}

// CheckDomainAvailability asks whether a name can be registered, with its first year's price.
func (c *Client) CheckDomainAvailability(ctx context.Context, name string) (*DomainAvailability, error) {
	raw, err := c.doRaw(ctx, routeCheckDomainAvailability, Params{}, url.Values{"domain": {name}}, nil)
	if err != nil {
		return nil, err
	}
	var out DomainAvailability
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s %s: decoding the response: %w", routeCheckDomainAvailability.method,
			routeCheckDomainAvailability.template, err)
	}
	return &out, nil
}

// OrderDomainRegistration orders the registration of a name for one year. The order is billed:
// it is never retried blindly.
func (c *Client) OrderDomainRegistration(ctx context.Context, scope Scope, name string) (*DomainOrder, error) {
	body := struct {
		DomainName string `json:"domain_name"`
		OrderType  string `json:"order_type"`
	}{DomainName: name, OrderType: DomainOrderRegister}
	var out DomainOrder
	if err := c.do(ctx, routeCreateDomainOrder, scope.Params(), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
