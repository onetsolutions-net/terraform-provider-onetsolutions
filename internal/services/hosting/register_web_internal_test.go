package hosting

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/plantest"
)

const (
	testOrg     = "3f2a9c1e-5b7d-4e8f-9a0b-1c2d3e4f5a6b"
	testProject = "8b1d4e2f-6c3a-4f9e-8d7b-2a1c0e9f8d7c"
	testHosting = "9a0b1c2d-3e4f-4a5b-8c6d-7e8f9a0b1c2d"
)

func TestParseWebChildImportID(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]webChildImportID{
		testOrg + "/" + testProject + "/" + testHosting + "/blog.example.com": {testOrg, testProject, testHosting, "blog.example.com"},
		testHosting + "/blog": {HostingID: testHosting, Key: "blog"},
		testOrg + "/" + testProject + "/" + testHosting + "/example.com/a/b": {testOrg, testProject, testHosting, "example.com/a/b"},
		testHosting + "/example.com/":                                        {HostingID: testHosting, Key: "example.com/"},
	} {
		got, err := parseWebChildImportID(raw, "key")
		if err != nil || got != want {
			t.Errorf("parseWebChildImportID(%q) = %+v, %v; want %+v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "blog", testHosting, testHosting + "/", "not-a-uuid/blog", testOrg + "/" + testProject + "/" + testHosting + "/"} {
		if _, err := parseWebChildImportID(raw, "key"); err == nil {
			t.Errorf("parseWebChildImportID(%q) accepted", raw)
		}
	}
}

func TestSameDirectory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		configured, reported string
		same                 bool
	}{
		{"public_html/site", "/home/acme/public_html/site", true},
		{"/public_html/site", "/home/acme/public_html/site", true},
		{"/home/acme/site/", "/home/acme/site", true},
		{"site", "/home/acme/public_html/site", true},
		{"public_html/other", "/home/acme/public_html/site", false},
		{"/", "/home/acme", false},
	} {
		if got := sameDirectory(tc.configured, tc.reported); got != tc.same {
			t.Errorf("sameDirectory(%q, %q) = %v", tc.configured, tc.reported, got)
		}
	}
}

func TestRedirectTypeAndID(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"301": "301", "permanent": "301", "302": "302", "temp": "302", "Temporary": "302", "307": "307"} {
		if got := redirectType(in); got != want {
			t.Errorf("redirectType(%q) = %q, want %q", in, got, want)
		}
	}
	if id := redirectID("Example.com", "old"); id != "example.com/old" {
		t.Errorf("redirectID = %q", id)
	}
	if domain, source := splitRedirectID("example.com/"); domain != "example.com" || source != "/" {
		t.Errorf("splitRedirectID = %q, %q", domain, source)
	}
}

func TestRedirectDestroyWarns(t *testing.T) {
	t.Parallel()
	r, ok := NewRedirectResource().(resource.ResourceWithModifyPlan)
	if !ok {
		t.Fatal("the redirect resource has no ModifyPlan")
	}
	redirect := map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, "example.com/old"), "domain": tftypes.NewValue(tftypes.String, "example.com"),
	}
	warned := func(resp *resource.ModifyPlanResponse) bool {
		for _, w := range resp.Diagnostics.Warnings() {
			if w.Detail() == RedirectRemovalWarning {
				return true
			}
		}
		return false
	}
	if resp := plantest.ModifyPlan(t, r, redirect, nil); !warned(resp) {
		t.Errorf("destroy did not warn: %v", resp.Diagnostics)
	}
	if resp := plantest.ModifyPlan(t, r, redirect, redirect); warned(resp) {
		t.Errorf("an unchanged plan warned: %v", resp.Diagnostics)
	}
}
