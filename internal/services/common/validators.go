package common

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// UUID validates that a string is a UUID in the canonical form the API returns: lowercase
// "8-4-4-4-12" hexadecimal, so state always equals configuration.
func UUID() validator.String { return uuidValidator{} }

// CIDR validates an IPv4 or IPv6 network in CIDR notation ("10.0.0.0/16", "2001:db8::/32").
func CIDR() validator.String { return cidrValidator{} }

// IPv4CIDR validates an IPv4 network in CIDR notation.
func IPv4CIDR() validator.String { return cidrValidator{ipv4Only: true} }

// PortSpec validates the port syntax of the API: a port ("443"), a range ("1000-2000"), or a
// comma-separated list of both, every port between 1 and 65535.
func PortSpec() validator.String { return portSpecValidator{} }

// NoSurroundingWhitespace rejects leading and trailing whitespace, which the API trims from its
// text fields: the stored value would differ from the configuration (plan section 5, no
// normalisation).
func NoSurroundingWhitespace() validator.String { return trimmedValidator{} }

type stringCheck func(string) error

type trimmedValidator struct{}

func (trimmedValidator) Description(context.Context) string {
	return "value must not start or end with whitespace"
}

func (v trimmedValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (trimmedValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	validate(req, resp, "Surrounding whitespace", func(value string) error {
		if strings.TrimSpace(value) != value {
			return fmt.Errorf("%q starts or ends with whitespace, which the API removes: remove it (for a heredoc, use chomp())", value)
		}
		return nil
	})
}

func validate(req validator.StringRequest, resp *validator.StringResponse, summary string, check stringCheck) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := check(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, summary, err.Error())
	}
}

type uuidValidator struct{}

func (uuidValidator) Description(context.Context) string { return "value must be a UUID" }

func (v uuidValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (uuidValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	validate(req, resp, "Invalid UUID", checkUUID)
}

// IsUUID reports whether value is a canonical lowercase UUID.
func IsUUID(value string) bool { return checkUUID(value) == nil }

func checkUUID(value string) error {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return fmt.Errorf("%q is not a lowercase UUID such as 3f2a9c1e-5b7d-4e8f-9a0b-1c2d3e4f5a6b", value)
	}
	return nil
}

type cidrValidator struct{ ipv4Only bool }

func (v cidrValidator) Description(context.Context) string {
	if v.ipv4Only {
		return "value must be an IPv4 network in CIDR notation"
	}
	return "value must be a network in CIDR notation"
}

func (v cidrValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v cidrValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	validate(req, resp, "Invalid CIDR", func(value string) error {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return fmt.Errorf("%q is not in CIDR notation, such as 10.0.0.0/16", value)
		}
		if v.ipv4Only && !prefix.Addr().Is4() {
			return fmt.Errorf("%q is not an IPv4 network", value)
		}
		if prefix.Masked() != prefix {
			return fmt.Errorf("%q has host bits set; the network is %s", value, prefix.Masked())
		}
		return nil
	})
}

type portSpecValidator struct{}

func (portSpecValidator) Description(context.Context) string {
	return "value must be a port, a range such as 1000-2000, or a comma-separated list of both, between 1 and 65535"
}

func (v portSpecValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (portSpecValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	validate(req, resp, "Invalid port", CheckPortSpec)
}

// CheckPortSpec is the check behind PortSpec, exported for validators of nested attributes.
func CheckPortSpec(value string) error {
	if value == "" {
		return fmt.Errorf("the port is empty; leave the attribute unset for all ports")
	}
	for _, part := range strings.Split(value, ",") {
		bounds := strings.SplitN(part, "-", 2)
		low, err := parsePort(bounds[0])
		if err != nil {
			return fmt.Errorf("%q: %w", value, err)
		}
		if len(bounds) == 2 {
			high, err := parsePort(bounds[1])
			if err != nil {
				return fmt.Errorf("%q: %w", value, err)
			}
			if high < low {
				return fmt.Errorf("%q: range %s ends before it starts", value, part)
			}
		}
	}
	return nil
}

func parsePort(raw string) (int, error) {
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != raw {
		return 0, fmt.Errorf("%q is not a port between 1 and 65535", raw)
	}
	return port, nil
}
