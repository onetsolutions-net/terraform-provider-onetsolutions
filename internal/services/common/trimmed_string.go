package common

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TrimmedStringType is a string type whose values are semantically equal when they differ only
// by surrounding whitespace, e.g. a public key read with file() and its trailing newline. State
// keeps the configured spelling; a different spelling read from the API is neither a diff nor an
// inconsistent result. Use it as the CustomType of the attribute and TrimmedString in the model.
type TrimmedStringType struct {
	basetypes.StringType
}

var _ basetypes.StringTypable = TrimmedStringType{}

// String describes the type.
func (t TrimmedStringType) String() string { return "common.TrimmedStringType" }

// ValueType returns the value type.
func (t TrimmedStringType) ValueType(context.Context) attr.Value { return TrimmedString{} }

// Equal reports whether o is a TrimmedStringType.
func (t TrimmedStringType) Equal(o attr.Type) bool {
	other, ok := o.(TrimmedStringType)
	return ok && t.StringType.Equal(other.StringType)
}

// ValueFromString wraps a StringValue.
func (t TrimmedStringType) ValueFromString(_ context.Context, in basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return TrimmedString{StringValue: in}, nil
}

// ValueFromTerraform converts a Terraform value.
func (t TrimmedStringType) ValueFromTerraform(ctx context.Context, in tftypes.Value) (attr.Value, error) {
	value, err := t.StringType.ValueFromTerraform(ctx, in)
	if err != nil {
		return nil, err
	}
	stringValue, ok := value.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T", value)
	}
	valuable, diags := t.ValueFromString(ctx, stringValue)
	if diags.HasError() {
		return nil, fmt.Errorf("converting StringValue to TrimmedString: %v", diags)
	}
	return valuable, nil
}

// TrimmedString is a value of TrimmedStringType.
type TrimmedString struct {
	basetypes.StringValue
}

var _ basetypes.StringValuableWithSemanticEquals = TrimmedString{}

// NewTrimmedString returns a known value.
func NewTrimmedString(value string) TrimmedString {
	return TrimmedString{StringValue: basetypes.NewStringValue(value)}
}

// NewTrimmedStringNull returns a null value.
func NewTrimmedStringNull() TrimmedString {
	return TrimmedString{StringValue: basetypes.NewStringNull()}
}

// Type returns TrimmedStringType.
func (v TrimmedString) Type(context.Context) attr.Type { return TrimmedStringType{} }

// Equal reports strict equality, as Terraform requires.
func (v TrimmedString) Equal(o attr.Value) bool {
	other, ok := o.(TrimmedString)
	return ok && v.StringValue.Equal(other.StringValue)
}

// StringSemanticEquals ignores surrounding whitespace.
func (v TrimmedString) StringSemanticEquals(_ context.Context, newValuable basetypes.StringValuable) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	newValue, ok := newValuable.(TrimmedString)
	if !ok {
		diags.AddError("Semantic equality check error",
			fmt.Sprintf("Expected TrimmedString, got %T. This is a bug in the provider; please report it.", newValuable))
		return false, diags
	}
	return strings.TrimSpace(v.ValueString()) == strings.TrimSpace(newValue.ValueString()), diags
}
