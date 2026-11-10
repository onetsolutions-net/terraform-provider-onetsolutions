package dns

import (
	"net/netip"
	"slices"
	"strings"
)

// The DNS service stores record names as lowercase FQDNs and returns TXT contents quoted and
// host targets without their trailing dot, whatever spelling the record was created with. The
// functions below decide whether two spellings mean the same record, as the API's
// canonical_name does, so the state keeps the configured spelling and a plan after apply is
// empty. Names depend on the zone, which a value type cannot see, so the resource applies them
// when it reads the record rather than through a custom type's semantic equality.

var hostTargetTypes = []string{"CNAME", "MX", "NS", "PTR"}

// canonicalName reduces a record name to the form the API compares: "@" for the apex, the
// label(s) relative to the zone otherwise.
func canonicalName(name, zoneName string) string {
	cleaned := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	zone := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zoneName), "."))
	if cleaned == "" || cleaned == "@" || cleaned == zone {
		return "@"
	}
	return strings.TrimSuffix(cleaned, "."+zone)
}

func sameName(a, b, zoneName string) bool {
	return canonicalName(a, zoneName) == canonicalName(b, zoneName)
}

// canonicalContent reduces a content to the form the API compares for its record type.
func canonicalContent(recordType, content string) string {
	trimmed := strings.TrimSpace(content)
	switch {
	case recordType == "TXT":
		return unquoteTXT(trimmed)
	case slices.Contains(hostTargetTypes, recordType):
		return strings.ToLower(strings.TrimSuffix(trimmed, "."))
	case recordType == "SRV":
		return srvTarget(trimmed)
	case recordType == "A" || recordType == "AAAA":
		if addr, err := netip.ParseAddr(trimmed); err == nil {
			return addr.String()
		}
		return trimmed
	default:
		return trimmed
	}
}

func sameContent(recordType, a, b string) bool {
	return canonicalContent(recordType, a) == canonicalContent(recordType, b)
}

// unquoteTXT returns the text of a TXT value written as one or more quoted strings separated by
// spaces, concatenated, with \" and \\ unescaped: a TXT string holds at most 255 bytes, so the
// DNS service returns a longer value split into several ("a" "b"), and a resolver joins them.
// A value that is not such a sequence is returned as is.
func unquoteTXT(value string) string {
	var text strings.Builder
	rest := strings.TrimSpace(value)
	if rest == "" {
		return value
	}
	for rest != "" {
		if rest[0] != '"' {
			return value
		}
		end, part, ok := quotedString(rest)
		if !ok {
			return value
		}
		text.WriteString(part)
		rest = rest[end:]
		trimmed := strings.TrimLeft(rest, " \t")
		if trimmed != "" && len(trimmed) == len(rest) {
			return value
		}
		rest = trimmed
	}
	return text.String()
}

// quotedString reads the quoted string value starts with and returns the index after its closing
// quote and its unescaped text; ok is false when it is not closed.
func quotedString(value string) (end int, text string, ok bool) {
	var b strings.Builder
	for i := 1; i < len(value); i++ {
		switch value[i] {
		case '\\':
			if i+1 == len(value) {
				return 0, "", false
			}
			i++
			b.WriteByte(value[i])
		case '"':
			return i + 1, b.String(), true
		default:
			b.WriteByte(value[i])
		}
	}
	return 0, "", false
}

// srvTarget lowercases an SRV content and removes the trailing dot of its target.
func srvTarget(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return value
	}
	last := len(fields) - 1
	fields[last] = strings.ToLower(strings.TrimSuffix(fields[last], "."))
	return strings.Join(fields, " ")
}

// importedName is the spelling written to the state when no configured spelling exists (after
// an import): "@" for the apex, the relative label(s) otherwise.
func importedName(name, zoneName string) string {
	return canonicalName(name, zoneName)
}

// importedContent is the spelling written to the state after an import: a TXT value unquoted,
// anything else as the API returns it.
func importedContent(recordType, content string) string {
	if recordType == "TXT" {
		return unquoteTXT(strings.TrimSpace(content))
	}
	return content
}
