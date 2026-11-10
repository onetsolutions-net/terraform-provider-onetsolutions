package common

import (
	"context"

	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// Names of the attributes every project-scoped object carries.
const (
	AttrID             = "id"
	AttrOrganizationID = "organization_id"
	AttrProjectID      = "project_id"
)

// IDAttribute is the computed `id` of a resource: the API's UUID, stable for the object's life.
func IDAttribute(what string) schema.StringAttribute {
	return schema.StringAttribute{
		Computed:            true,
		MarkdownDescription: "The UUID of the " + what + ".",
		PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

// ScopeAttributes returns organization_id and project_id for a project-scoped resource, to merge
// into its schema attributes. Both are Optional+Computed: on create they default to the
// provider's settings (ResourceBase.PlanScope), afterwards they keep the value of the state when
// the configuration leaves them unset, and they force a replacement only when set explicitly to
// a value that differs from the state.
func ScopeAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		AttrOrganizationID: schema.StringAttribute{
			Optional: true,
			Computed: true,
			MarkdownDescription: "The organization the object belongs to. Defaults to the provider's `organization_id` " +
				"when the object is created; changing it explicitly recreates the object.",
			Validators:    []validator.String{UUID()},
			PlanModifiers: []planmodifier.String{scopeModifier{}},
		},
		AttrProjectID: schema.StringAttribute{
			Optional: true,
			Computed: true,
			MarkdownDescription: "The project the object belongs to. Defaults to the provider's `project_id` " +
				"when the object is created; changing it explicitly recreates the object.",
			Validators:    []validator.String{UUID()},
			PlanModifiers: []planmodifier.String{scopeModifier{}},
		},
	}
}

// DataSourceScopeAttributes returns organization_id and project_id for a project-scoped data
// source: where to look, defaulting to the provider's settings.
func DataSourceScopeAttributes() map[string]dsschema.Attribute {
	return map[string]dsschema.Attribute{
		AttrOrganizationID: DataSourceOrganizationAttribute(),
		AttrProjectID: dsschema.StringAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: "The project to read from. Defaults to the provider's `project_id`.",
			Validators:          []validator.String{UUID()},
		},
	}
}

// DataSourceOrganizationAttribute is organization_id for a data source scoped to an organization.
func DataSourceOrganizationAttribute() dsschema.StringAttribute {
	return dsschema.StringAttribute{
		Optional:            true,
		Computed:            true,
		MarkdownDescription: "The organization to read from. Defaults to the provider's `organization_id`.",
		Validators:          []validator.String{UUID()},
	}
}

// MergeAttributes merges attribute maps into a new one; a name defined twice is a programming
// error and panics at schema time, so it never reaches a user.
func MergeAttributes[A any](maps ...map[string]A) map[string]A {
	merged := map[string]A{}
	for _, m := range maps {
		for name, attribute := range m {
			if _, exists := merged[name]; exists {
				panic("attribute " + name + " defined twice")
			}
			merged[name] = attribute
		}
	}
	return merged
}

type scopeModifier struct{}

func (scopeModifier) Description(context.Context) string {
	return "Keeps the value of the state unless the configuration sets a different one, which forces a replacement."
}

func (m scopeModifier) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

func (scopeModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if req.ConfigValue.IsNull() {
		if !req.StateValue.IsNull() {
			resp.PlanValue = req.StateValue
		}
		return
	}
	if req.StateValue.IsNull() {
		return
	}
	if req.ConfigValue.IsUnknown() || !req.ConfigValue.Equal(req.StateValue) {
		resp.RequiresReplace = true
	}
}
