package hosting

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/plantest"
)

func warnedWith(resp *resource.ModifyPlanResponse, text string) bool {
	for _, w := range resp.Diagnostics.Warnings() {
		if strings.Contains(w.Detail(), text) || strings.Contains(w.Summary(), text) {
			return true
		}
	}
	return false
}

func TestEmailAccountReplacementWarns(t *testing.T) {
	t.Parallel()
	r, ok := NewEmailAccountResource().(resource.ResourceWithModifyPlan)
	if !ok {
		t.Fatal("the email account resource has no ModifyPlan")
	}
	account := func(email string) map[string]tftypes.Value {
		return map[string]tftypes.Value{
			"id":              tftypes.NewValue(tftypes.String, "contact@example.com"),
			"organization_id": tftypes.NewValue(tftypes.String, "org"),
			"project_id":      tftypes.NewValue(tftypes.String, "project"),
			"hosting_id":      tftypes.NewValue(tftypes.String, "hosting"),
			"email":           tftypes.NewValue(tftypes.String, email),
		}
	}
	if resp := plantest.ModifyPlan(t, r, account("contact@example.com"), account("sales@example.com")); !warnedWith(resp, "Replacing") {
		t.Errorf("a new address did not warn that the mailbox is deleted: %v", resp.Diagnostics)
	}
	if resp := plantest.ModifyPlan(t, r, account("contact@example.com"), account("contact@example.com")); warnedWith(resp, "deletes the mailbox") {
		t.Errorf("an unchanged mailbox warned: %v", resp.Diagnostics)
	}
}

func TestFTPAccountReplacementSkipsThePasswordWarning(t *testing.T) {
	t.Parallel()
	r, ok := NewFTPAccountResource().(resource.ResourceWithModifyPlan)
	if !ok {
		t.Fatal("the FTP account resource has no ModifyPlan")
	}
	account := func(username, password string) map[string]tftypes.Value {
		values := map[string]tftypes.Value{
			"id":              tftypes.NewValue(tftypes.String, "deploy"),
			"organization_id": tftypes.NewValue(tftypes.String, "org"),
			"project_id":      tftypes.NewValue(tftypes.String, "project"),
			"hosting_id":      tftypes.NewValue(tftypes.String, "hosting"),
			"username":        tftypes.NewValue(tftypes.String, username),
		}
		if password != "" {
			values["password"] = tftypes.NewValue(tftypes.String, password)
		}
		return values
	}
	if resp := plantest.ModifyPlan(t, r, account("deploy", ""), account("deploy", "Str0ng-Passw0rd")); !warnedWith(resp, "password will be set") {
		t.Errorf("setting the password of an imported account did not warn: %v", resp.Diagnostics)
	}
	if resp := plantest.ModifyPlan(t, r, account("deploy", ""), account("release", "Str0ng-Passw0rd")); warnedWith(resp, "password will be set") {
		t.Errorf("a replaced account warned about its password: %v", resp.Diagnostics)
	}
}
