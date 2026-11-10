package common

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

func TestParseImportID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw        string
		withParent bool
		want       ImportID
		wantErr    bool
	}{
		{raw: "o/p/i", want: ImportID{OrganizationID: "o", ProjectID: "p", ID: "i"}},
		{raw: "i", want: ImportID{ID: "i"}},
		{raw: "o/p/z/i", withParent: true, want: ImportID{OrganizationID: "o", ProjectID: "p", ParentID: "z", ID: "i"}},
		{raw: "z/i", withParent: true, want: ImportID{ParentID: "z", ID: "i"}},
		{raw: "o/p", wantErr: true},
		{raw: "o/p/z/i", wantErr: true},
		{raw: "i", withParent: true, wantErr: true},
		{raw: "o//i", wantErr: true},
		{raw: "", wantErr: true},
	}
	for _, tc := range cases {
		got, err := ParseImportID(tc.raw, tc.withParent)
		if (err != nil) != tc.wantErr || (!tc.wantErr && got != tc.want) {
			t.Errorf("ParseImportID(%q, %v) = %+v, %v", tc.raw, tc.withParent, got, err)
		}
	}
}

func runStringValidator(v validator.String, value string) diag.Diagnostics {
	resp := &validator.StringResponse{}
	v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("x"), ConfigValue: types.StringValue(value)}, resp)
	return resp.Diagnostics
}

func TestValidators(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		v     validator.String
		valid []string
		bad   []string
	}{
		{"UUID", UUID(), []string{"3f2a9c1e-5b7d-4e8f-9a0b-1c2d3e4f5a6b"},
			[]string{"3F2A9C1E-5B7D-4E8F-9A0B-1C2D3E4F5A6B", "3f2a9c1e5b7d4e8f9a0b1c2d3e4f5a6b", "nope"}},
		{"CIDR", CIDR(), []string{"10.0.0.0/16", "0.0.0.0/0", "2001:db8::/32", "192.0.2.7/32"},
			[]string{"10.0.0.1/16", "10.0.0.0", "300.0.0.0/8"}},
		{"IPv4CIDR", IPv4CIDR(), []string{"172.16.0.0/12"}, []string{"2001:db8::/32"}},
		{"PortSpec", PortSpec(), []string{"22", "1000-2000", "80,443,8000-8100", "65535"},
			[]string{"", "0", "65536", "2000-1000", "22 ", "080", "a-b", "1,,2"}},
		{"NoSurroundingWhitespace", NoSurroundingWhitespace(), []string{"", "allow ssh", "a b"},
			[]string{" a", "a ", "allow ssh\n", "\ta"}},
	}
	for _, tc := range cases {
		for _, value := range tc.valid {
			if diags := runStringValidator(tc.v, value); diags.HasError() {
				t.Errorf("%s rejected %q: %v", tc.name, value, diags)
			}
		}
		for _, value := range tc.bad {
			if diags := runStringValidator(tc.v, value); !diags.HasError() {
				t.Errorf("%s accepted %q", tc.name, value)
			}
		}
	}
	resp := &validator.StringResponse{}
	UUID().ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringUnknown()}, resp)
	if resp.Diagnostics.HasError() {
		t.Error("an unknown value must not be validated")
	}
}

func TestTrimmedStringSemanticEquality(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	equal, diags := NewTrimmedString("ssh-ed25519 AAAA\n").StringSemanticEquals(ctx, NewTrimmedString("  ssh-ed25519 AAAA"))
	if !equal || diags.HasError() {
		t.Fatal("surrounding whitespace must not matter")
	}
	equal, _ = NewTrimmedString("ssh-ed25519 AAAA").StringSemanticEquals(ctx, NewTrimmedString("ssh-ed25519 BBBB"))
	if equal {
		t.Fatal("different keys compared equal")
	}
	if NewTrimmedString("a").Equal(NewTrimmedString("a ")) {
		t.Fatal("Equal must stay strict")
	}
	value, err := TrimmedStringType{}.ValueFromTerraform(ctx, tftypes.NewValue(tftypes.String, "k"))
	if err != nil || !value.Equal(NewTrimmedString("k")) {
		t.Fatalf("ValueFromTerraform = %v, %v", value, err)
	}
}

var testSchema = schema.Schema{Attributes: map[string]schema.Attribute{
	"id": schema.StringAttribute{Computed: true},
	"x":  schema.StringAttribute{Optional: true},
}}

func objectValue(id, x *string) tftypes.Value {
	typ := testSchema.Type().TerraformType(context.Background())
	str := func(s *string) tftypes.Value {
		if s == nil {
			return tftypes.NewValue(tftypes.String, nil)
		}
		return tftypes.NewValue(tftypes.String, *s)
	}
	return tftypes.NewValue(typ, map[string]tftypes.Value{"id": str(id), "x": str(x)})
}

func ptr(s string) *string { return &s }

func createOnlyRequest(state, plan tftypes.Value, stateValue, planValue types.String) planmodifier.StringRequest {
	return planmodifier.StringRequest{
		Path:       path.Root("x"),
		State:      tfsdk.State{Schema: testSchema, Raw: state},
		Plan:       tfsdk.Plan{Schema: testSchema, Raw: plan},
		StateValue: stateValue,
		PlanValue:  planValue,
	}
}

func TestCreateOnlyModifiers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	nullState := tftypes.NewValue(testSchema.Type().TerraformType(ctx), nil)
	existing := objectValue(ptr("i"), ptr("old"))
	createdWithout := objectValue(ptr("i"), nil)
	planned := objectValue(ptr("i"), ptr("new"))
	modifier := CreateOnlyWarn("It takes effect on a reinstall.")

	cases := []struct {
		name        string
		req         planmodifier.StringRequest
		wantWarning bool
	}{
		{"create", createOnlyRequest(nullState, planned, types.StringNull(), types.StringValue("new")), false},
		{"unchanged", createOnlyRequest(existing, existing, types.StringValue("old"), types.StringValue("old")), false},
		{"changed", createOnlyRequest(existing, planned, types.StringValue("old"), types.StringValue("new")), true},
		{"set after a create without it", createOnlyRequest(createdWithout, planned, types.StringNull(), types.StringValue("new")), true},
		{"removed", createOnlyRequest(existing, createdWithout, types.StringValue("old"), types.StringNull()), true},
		{"unknown", createOnlyRequest(existing, planned, types.StringValue("old"), types.StringUnknown()), false},
	}
	for _, tc := range cases {
		resp := &planmodifier.StringResponse{PlanValue: tc.req.PlanValue}
		modifier.PlanModifyString(ctx, tc.req, resp)
		if resp.RequiresReplace || (resp.Diagnostics.WarningsCount() > 0) != tc.wantWarning {
			t.Errorf("%s: replace = %v, diagnostics = %v", tc.name, resp.RequiresReplace, resp.Diagnostics)
		}
		if tc.wantWarning && !strings.Contains(resp.Diagnostics.Warnings()[0].Detail(), "It takes effect on a reinstall.") {
			t.Errorf("%s: warning = %v", tc.name, resp.Diagnostics)
		}
	}
}

// The private state of an imported object makes every change silent until the next apply.
func TestCreateOnlyModifierAfterImport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	private := &fakePrivate{}
	if diags := MarkImported(ctx, private); diags.HasError() || !imported(ctx, private) {
		t.Fatalf("MarkImported: %v, imported = %v", diags, imported(ctx, private))
	}
	m := CreateOnlyWarn("")
	if diags := m.warning(false, imported(ctx, private), types.StringNull(), types.StringValue("new"), path.Root("x")); diags.WarningsCount() != 0 {
		t.Fatalf("warned after an import: %v", diags)
	}
	if diags := ClearImported(ctx, private); diags.HasError() || imported(ctx, private) {
		t.Fatalf("ClearImported: %v, imported = %v", diags, imported(ctx, private))
	}
	if diags := m.warning(false, imported(ctx, private), types.StringNull(), types.StringValue("new"), path.Root("x")); diags.WarningsCount() != 1 {
		t.Fatalf("no warning once the import was applied: %v", diags)
	}
}

type fakePrivate struct{ data map[string][]byte }

func (p *fakePrivate) GetKey(_ context.Context, key string) ([]byte, diag.Diagnostics) {
	return p.data[key], nil
}

func (p *fakePrivate) SetKey(_ context.Context, key string, value []byte) diag.Diagnostics {
	if p.data == nil {
		p.data = map[string][]byte{}
	}
	if len(value) == 0 {
		delete(p.data, key)
	} else {
		p.data[key] = value
	}
	return nil
}

func TestScopeModifier(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	nullState := tftypes.NewValue(testSchema.Type().TerraformType(ctx), nil)
	existing := objectValue(ptr("i"), ptr("p1"))
	cases := []struct {
		name        string
		state       tftypes.Value
		config      types.String
		stateValue  types.String
		wantPlan    types.String
		wantReplace bool
	}{
		{"create keeps unknown", nullState, types.StringNull(), types.StringNull(), types.StringUnknown(), false},
		{"unset keeps state", existing, types.StringNull(), types.StringValue("p1"), types.StringValue("p1"), false},
		{"same explicit value", existing, types.StringValue("p1"), types.StringValue("p1"), types.StringUnknown(), false},
		{"explicit different value", existing, types.StringValue("p2"), types.StringValue("p1"), types.StringUnknown(), true},
	}
	for _, tc := range cases {
		req := planmodifier.StringRequest{
			State: tfsdk.State{Schema: testSchema, Raw: tc.state}, Plan: tfsdk.Plan{Schema: testSchema, Raw: existing},
			ConfigValue: tc.config, StateValue: tc.stateValue, PlanValue: types.StringUnknown(),
		}
		resp := &planmodifier.StringResponse{PlanValue: types.StringUnknown()}
		scopeModifier{}.PlanModifyString(ctx, req, resp)
		if !resp.PlanValue.Equal(tc.wantPlan) || resp.RequiresReplace != tc.wantReplace {
			t.Errorf("%s: plan = %v, replace = %v", tc.name, resp.PlanValue, resp.RequiresReplace)
		}
	}
}

func TestFindOne(t *testing.T) {
	t.Parallel()
	items := []string{"a", "b", "b"}
	id := func(s string) string { return s }
	if got, diags := FindOne(items, func(s string) bool { return s == "a" }, id, "thing", "name a"); diags.HasError() || got != "a" {
		t.Fatalf("got %q, %v", got, diags)
	}
	if _, diags := FindOne(items, func(s string) bool { return s == "b" }, id, "thing", "name b"); !diags.HasError() ||
		!strings.Contains(diags.Errors()[0].Detail(), "b, b") {
		t.Fatalf("several matches: %v", diags)
	}
	if _, diags := FindOne(items, func(string) bool { return false }, id, "thing", "name z"); !diags.HasError() {
		t.Fatal("no match must be an error")
	}
}

func TestErrorDetail(t *testing.T) {
	t.Parallel()
	apiErr := &client.APIError{StatusCode: 409, Method: "DELETE", Path: "/v1/x", Code: "conflict", Detail: "Busy.",
		Fields: map[string][]string{"name": {"Taken."}}}
	detail := ErrorDetail(apiErr)
	for _, want := range []string{"Busy.", "name: Taken.", "DELETE /v1/x, HTTP 409, code conflict"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail %q lacks %q", detail, want)
		}
	}
	timeout := &waiter.TimeoutError{Description: "instance x", LastState: "building"}
	for _, want := range []string{"stays in the state", "next apply replaces it", "places a new order", "terraform untaint"} {
		if !strings.Contains(ErrorDetail(timeout), want) {
			t.Errorf("a timeout must say the object is tracked and what the next apply does: %q lacks %q", ErrorDetail(timeout), want)
		}
	}
	if strings.Contains(ErrorDetail(timeout), "waits for it") {
		t.Error("no resource resumes a wait on the next apply")
	}
	if ErrorDetail(errors.New("plain")) != "plain" {
		t.Error("plain errors pass through")
	}

	var diags diag.Diagnostics
	AddClientErrorWithFields(&diags, "Could not create", &client.APIError{StatusCode: 400, Fields: map[string][]string{"name": {"Taken."}}},
		map[string]path.Path{"name": path.Root("name")})
	if diags.ErrorsCount() != 1 {
		t.Fatalf("diags = %v", diags)
	}
}

func TestSavePartialState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := schema.Schema{Attributes: map[string]schema.Attribute{
		AttrID: schema.StringAttribute{Computed: true}, AttrOrganizationID: schema.StringAttribute{Computed: true},
		AttrProjectID: schema.StringAttribute{Computed: true}, "status": schema.StringAttribute{Computed: true},
		"name": schema.StringAttribute{Required: true},
	}}
	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	diags := SavePartialState(ctx, &state, "i", client.Scope{OrganizationID: "o", ProjectID: "p"},
		map[string]attr.Value{"status": types.StringValue("pending")})
	if diags.HasError() {
		t.Fatal(diags)
	}
	var id, status, name types.String
	state.GetAttribute(ctx, path.Root(AttrID), &id)
	state.GetAttribute(ctx, path.Root("status"), &status)
	state.GetAttribute(ctx, path.Root("name"), &name)
	if id.ValueString() != "i" || status.ValueString() != "pending" || !name.IsNull() {
		t.Fatalf("id = %v, status = %v, name = %v", id, status, name)
	}
}
