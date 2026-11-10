package dns

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// recordStringType is the string type of a record's name or content. Its values are
// semantically equal when they differ only in a way the DNS service ignores whatever the zone
// and the record type: case and a trailing dot for a name; surrounding quotes, surrounding
// whitespace and the trailing dot of a host name for a content. The zone-aware equivalences
// (the apex as "@" or as the zone name, a relative or a fully qualified name) are applied by the
// resource when it reads the record.
type recordStringType struct {
	basetypes.StringType
	content bool
}

var (
	_ basetypes.StringTypable                    = recordStringType{}
	_ basetypes.StringValuableWithSemanticEquals = recordString{}
)

var (
	nameType    = recordStringType{}
	contentType = recordStringType{content: true}
)

func (t recordStringType) String() string {
	if t.content {
		return "dns.recordContentType"
	}
	return "dns.recordNameType"
}

func (t recordStringType) ValueType(context.Context) attr.Value {
	return recordString{content: t.content}
}

func (t recordStringType) Equal(o attr.Type) bool {
	other, ok := o.(recordStringType)
	return ok && other.content == t.content
}

func (t recordStringType) ValueFromString(_ context.Context, in basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return recordString{StringValue: in, content: t.content}, nil
}

func (t recordStringType) ValueFromTerraform(ctx context.Context, in tftypes.Value) (attr.Value, error) {
	value, err := t.StringType.ValueFromTerraform(ctx, in)
	if err != nil {
		return nil, err
	}
	stringValue, ok := value.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T", value)
	}
	return recordString{StringValue: stringValue, content: t.content}, nil
}

// recordString is a value of recordStringType.
type recordString struct {
	basetypes.StringValue
	content bool
}

func newName(value string) recordString {
	return recordString{StringValue: basetypes.NewStringValue(value)}
}

func newContent(value string) recordString {
	return recordString{StringValue: basetypes.NewStringValue(value), content: true}
}

func (v recordString) Type(context.Context) attr.Type { return recordStringType{content: v.content} }

func (v recordString) Equal(o attr.Value) bool {
	other, ok := o.(recordString)
	return ok && other.content == v.content && v.StringValue.Equal(other.StringValue)
}

func (v recordString) StringSemanticEquals(_ context.Context, newValuable basetypes.StringValuable) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	other, ok := newValuable.(recordString)
	if !ok {
		diags.AddError("Semantic equality check error",
			fmt.Sprintf("Expected a DNS record value, got %T. This is a bug in the provider; please report it.", newValuable))
		return false, diags
	}
	if v.content {
		return looseContent(v.ValueString()) == looseContent(other.ValueString()), diags
	}
	return looseName(v.ValueString()) == looseName(other.ValueString()), diags
}

func looseName(name string) string {
	cleaned := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if cleaned == "" {
		return "@"
	}
	return cleaned
}

// looseContent removes what no record type gives a meaning to: surrounding whitespace, the
// quotes of a single quoted string, and the trailing dot of a value shaped like a host name.
func looseContent(content string) string {
	value := unquoteTXT(strings.TrimSpace(content))
	if trimmed, isHost := strings.CutSuffix(value, "."); isHost && looksLikeHostName(trimmed) {
		return strings.ToLower(trimmed)
	}
	if looksLikeHostName(value) {
		return strings.ToLower(value)
	}
	return value
}

func looksLikeHostName(value string) bool {
	if !strings.Contains(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" {
			return false
		}
		for _, r := range label {
			isAlnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
			if !isAlnum && r != '-' && r != '_' {
				return false
			}
		}
	}
	return true
}
