package common

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ImportID is a parsed import identifier.
type ImportID struct {
	OrganizationID string
	ProjectID      string
	ParentID       string
	ID             string
}

// ParseImportID splits an import ID. Without a parent the accepted forms are
// "<organization_id>/<project_id>/<id>" and "<id>"; with a parent (child objects such as a DNS
// record) "<organization_id>/<project_id>/<parent_id>/<id>" and "<parent_id>/<id>". The short
// forms leave OrganizationID/ProjectID empty: the caller resolves them from the provider.
func ParseImportID(raw string, withParent bool) (ImportID, error) {
	parts := strings.Split(raw, "/")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return ImportID{}, fmt.Errorf("import ID %q has an empty part; %s", raw, importForms(withParent))
		}
	}
	switch {
	case !withParent && len(parts) == 3:
		return ImportID{OrganizationID: parts[0], ProjectID: parts[1], ID: parts[2]}, nil
	case !withParent && len(parts) == 1:
		return ImportID{ID: parts[0]}, nil
	case withParent && len(parts) == 4:
		return ImportID{OrganizationID: parts[0], ProjectID: parts[1], ParentID: parts[2], ID: parts[3]}, nil
	case withParent && len(parts) == 2:
		return ImportID{ParentID: parts[0], ID: parts[1]}, nil
	default:
		return ImportID{}, fmt.Errorf("unexpected import ID %q; %s", raw, importForms(withParent))
	}
}

func importForms(withParent bool) string {
	if withParent {
		return "expected <organization_id>/<project_id>/<parent_id>/<id>, or <parent_id>/<id> when the provider sets organization_id and project_id"
	}
	return "expected <organization_id>/<project_id>/<id>, or <id> when the provider sets organization_id and project_id"
}

// ImportScoped imports a project-scoped resource from "<organization_id>/<project_id>/<id>" or
// "<id>", writing id, organization_id and project_id to the state. Read fills the rest.
func (b *ResourceBase) ImportScoped(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	b.importState(ctx, req, resp, "")
}

// ImportScopedChild imports a child resource from
// "<organization_id>/<project_id>/<parent_id>/<id>" or "<parent_id>/<id>", writing the parent ID
// to parentAttribute.
func (b *ResourceBase) ImportScopedChild(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse, parentAttribute string) {
	b.importState(ctx, req, resp, parentAttribute)
}

func (b *ResourceBase) importState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse, parentAttribute string) {
	id, err := ParseImportID(req.ID, parentAttribute != "")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	if id.OrganizationID == "" {
		scope, diags := b.ResolveScope(ctx, types.StringNull(), types.StringNull())
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		id.OrganizationID, id.ProjectID = scope.OrganizationID, scope.ProjectID
	}
	var diags diag.Diagnostics
	diags.Append(resp.State.SetAttribute(ctx, path.Root(AttrID), id.ID)...)
	diags.Append(resp.State.SetAttribute(ctx, path.Root(AttrOrganizationID), id.OrganizationID)...)
	diags.Append(resp.State.SetAttribute(ctx, path.Root(AttrProjectID), id.ProjectID)...)
	if parentAttribute != "" {
		diags.Append(resp.State.SetAttribute(ctx, path.Root(parentAttribute), id.ParentID)...)
	}
	resp.Diagnostics.Append(diags...)
}
