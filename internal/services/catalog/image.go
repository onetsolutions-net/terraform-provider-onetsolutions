package catalog

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ datasource.DataSourceWithConfigure        = (*imageDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*imageDataSource)(nil)
	_ datasource.DataSourceWithConfigure        = (*imagesDataSource)(nil)
)

const (
	visibilityPublic  = "public"
	visibilityPrivate = "private"
)

const visibilityDescription = "`public` reads the public catalog only (no project needed), `private` the project's own " +
	"images only; when unset, both are searched in the project."

// imageFilter holds the optional filters shared by onetsolutions_image and onetsolutions_images.
type imageFilter struct {
	Slug           types.String
	OSName         types.String
	OSVersion      types.String
	Language       types.String
	OSArchitecture types.String
}

func (f imageFilter) matches(image *client.Image) bool {
	return image.IsActive &&
		(f.Slug.IsNull() || image.Slug == f.Slug.ValueString()) &&
		(f.OSName.IsNull() || strings.EqualFold(image.OSName, f.OSName.ValueString())) &&
		(f.OSVersion.IsNull() || image.OSVersion == f.OSVersion.ValueString()) &&
		(f.Language.IsNull() || strings.EqualFold(image.Language, f.Language.ValueString())) &&
		(f.OSArchitecture.IsNull() || image.OSArchitecture == f.OSArchitecture.ValueString())
}

func (f imageFilter) describe() string {
	var parts []string
	add := func(name string, v types.String) {
		if !v.IsNull() {
			parts = append(parts, fmt.Sprintf("%s %q", name, v.ValueString()))
		}
	}
	add("slug", f.Slug)
	add("os_name", f.OSName)
	add("os_version", f.OSVersion)
	add("language", f.Language)
	add("os_architecture", f.OSArchitecture)
	return strings.Join(parts, ", ")
}

type imageModel struct {
	ID                   types.String `tfsdk:"id"`
	Name                 types.String `tfsdk:"name"`
	Slug                 types.String `tfsdk:"slug"`
	Description          types.String `tfsdk:"description"`
	Visibility           types.String `tfsdk:"visibility"`
	OSName               types.String `tfsdk:"os_name"`
	OSVersion            types.String `tfsdk:"os_version"`
	OSArchitecture       types.String `tfsdk:"os_architecture"`
	Language             types.String `tfsdk:"language"`
	IsWindows            types.Bool   `tfsdk:"is_windows"`
	RequiresLicenseAddon types.Bool   `tfsdk:"requires_license_addon"`
	MinDiskGB            types.Int64  `tfsdk:"min_disk_gb"`
	MinRAMMB             types.Int64  `tfsdk:"min_ram_mb"`
	Created              types.String `tfsdk:"created"`
}

var imageAttrTypes = map[string]attr.Type{
	"id": types.StringType, "name": types.StringType, "slug": types.StringType, "description": types.StringType,
	"visibility": types.StringType, "os_name": types.StringType, "os_version": types.StringType,
	"os_architecture": types.StringType, "language": types.StringType, "is_windows": types.BoolType,
	"requires_license_addon": types.BoolType, "min_disk_gb": types.Int64Type, "min_ram_mb": types.Int64Type,
	"created": types.StringType,
}

func newImageModel(image *client.Image) imageModel {
	return imageModel{
		ID:                   types.StringValue(image.ID),
		Name:                 types.StringValue(image.Name),
		Slug:                 types.StringValue(image.Slug),
		Description:          types.StringValue(image.Description),
		Visibility:           types.StringValue(image.Visibility),
		OSName:               types.StringValue(image.OSName),
		OSVersion:            types.StringValue(image.OSVersion),
		OSArchitecture:       types.StringValue(image.OSArchitecture),
		Language:             types.StringValue(image.Language),
		IsWindows:            types.BoolValue(image.IsWindows),
		RequiresLicenseAddon: types.BoolValue(image.RequiresLicenseAddon),
		MinDiskGB:            types.Int64Value(image.MinDiskGB),
		MinRAMMB:             types.Int64Value(image.MinRAMMB),
		Created:              types.StringValue(image.Created),
	}
}

func imageComputedAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "The display name of the image, such as `Ubuntu 24.04`."},
		"description": schema.StringAttribute{Computed: true, MarkdownDescription: "The description of the image."},
		"is_windows":  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the image is a Windows system."},
		"requires_license_addon": schema.BoolAttribute{
			Computed:            true,
			MarkdownDescription: "Whether an instance installed with it must order a license add-on.",
		},
		"min_disk_gb": schema.Int64Attribute{Computed: true, MarkdownDescription: "The smallest disk it installs on, in GB."},
		"min_ram_mb":  schema.Int64Attribute{Computed: true, MarkdownDescription: "The least memory it runs with, in MB."},
		"created":     schema.StringAttribute{Computed: true, MarkdownDescription: "When the image was published (RFC 3339)."},
	}
}

// filterAttributes are the image attributes that also filter, Optional+Computed in a lookup.
func filterAttributes(lookup bool) map[string]schema.Attribute {
	alsoRequiresOSName := []validator.String{stringvalidator.AlsoRequires(path.MatchRoot("os_name"))}
	if !lookup {
		alsoRequiresOSName = nil
	}
	return map[string]schema.Attribute{
		"os_name": schema.StringAttribute{
			Optional: true, Computed: lookup,
			MarkdownDescription: "The operating system, such as `Ubuntu` or `Debian` (case-insensitive).",
		},
		"os_version": schema.StringAttribute{
			Optional: true, Computed: lookup,
			MarkdownDescription: "The version of the operating system, such as `24.04` or `13`.",
		},
		"language": schema.StringAttribute{
			Optional: true, Computed: lookup,
			MarkdownDescription: "The language of the system, such as `fr-FR` (case-insensitive); empty for Linux images.",
			Validators:          alsoRequiresOSName,
		},
		"os_architecture": schema.StringAttribute{
			Optional: true, Computed: lookup,
			MarkdownDescription: "The CPU architecture, such as `x86_64`.",
			Validators:          alsoRequiresOSName,
		},
		"visibility": schema.StringAttribute{
			Optional: true, Computed: lookup,
			MarkdownDescription: "Where to search: " + visibilityDescription,
			Validators:          []validator.String{stringvalidator.OneOf(visibilityPublic, visibilityPrivate)},
		},
	}
}

// listImages reads the images to search: the anonymous public catalog when visibility is
// "public", the project's images otherwise. scope is nil for the public catalog.
func listImages(ctx context.Context, base *common.DataSourceBase, c *client.Client, visibility string, organizationID, projectID types.String) ([]client.Image, *client.Scope, diag.Diagnostics) {
	var diags diag.Diagnostics
	if visibility == visibilityPublic {
		images, err := c.ListPublicImages(ctx)
		if err != nil {
			common.AddClientError(&diags, "Could not list the public images", err)
		}
		return images, nil, diags
	}
	scope, scopeDiags := base.ResolveScope(ctx, organizationID, projectID)
	diags.Append(scopeDiags...)
	if diags.HasError() {
		return nil, nil, diags
	}
	images, err := c.ListProjectImages(ctx, scope, visibility)
	if err != nil {
		common.AddClientError(&diags, "Could not list the images", err)
	}
	return images, &scope, diags
}

// scopeValues returns the scope read from, or the configured values when the public catalog
// was read without one.
func scopeValues(scope *client.Scope, organizationID, projectID types.String) (org, project types.String) {
	if scope == nil {
		return organizationID, projectID
	}
	return types.StringValue(scope.OrganizationID), types.StringValue(scope.ProjectID)
}

// NewImageDataSource returns the onetsolutions_image data source.
func NewImageDataSource() datasource.DataSource {
	return &imageDataSource{}
}

type imageDataSource struct {
	common.DataSourceBase
}

type imageLookupModel struct {
	imageModel
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	MostRecent     types.Bool   `tfsdk:"most_recent"`
}

func (d *imageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image"
}

func (d *imageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an image to install instances with, by `id`, by `slug`, or by `os_name` and `os_version`.",
		Attributes: common.MergeAttributes(imageComputedAttributes(), filterAttributes(true), common.DataSourceScopeAttributes(),
			map[string]schema.Attribute{
				"id": schema.StringAttribute{
					Optional:            true,
					Computed:            true,
					MarkdownDescription: "The UUID of the image. Exactly one of `id`, `slug` and `os_name` is required.",
					Validators:          []validator.String{common.UUID()},
				},
				"slug": schema.StringAttribute{
					Optional:            true,
					Computed:            true,
					MarkdownDescription: "The slug of the image, such as `ubuntu-2404`. Exactly one of `id`, `slug` and `os_name` is required.",
				},
				"most_recent": schema.BoolAttribute{
					Optional:            true,
					MarkdownDescription: "When several images match, pick the most recently published one instead of failing.",
				},
			}),
	}
}

func (d *imageDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("slug"), path.MatchRoot("os_name")),
		datasourcevalidator.RequiredTogether(path.MatchRoot("os_name"), path.MatchRoot("os_version")),
	}
}

func (d *imageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config imageLookupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	visibility := config.Visibility.ValueString()

	var (
		image *client.Image
		scope *client.Scope
	)
	switch {
	case !config.ID.IsNull() && visibility != visibilityPublic:
		resolved, diags := d.ResolveScope(ctx, config.OrganizationID, config.ProjectID)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		found, err := c.GetProjectImage(ctx, resolved, config.ID.ValueString())
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not read the image", err)
			return
		}
		if visibility != "" && found.Visibility != visibility {
			resp.Diagnostics.AddError("No image found",
				fmt.Sprintf("Image %s is %s, not %s.", found.ID, found.Visibility, visibility))
			return
		}
		image, scope = found, &resolved
	default:
		images, listScope, diags := listImages(ctx, &d.DataSourceBase, c, visibility, config.OrganizationID, config.ProjectID)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		scope = listScope
		found, diags := pickImage(images, config)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		image = found
	}

	model := imageLookupModel{imageModel: newImageModel(image), MostRecent: config.MostRecent}
	model.OrganizationID, model.ProjectID = scopeValues(scope, config.OrganizationID, config.ProjectID)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func pickImage(images []client.Image, config imageLookupModel) (*client.Image, diag.Diagnostics) {
	var diags diag.Diagnostics
	filter := imageFilter{
		Slug: config.Slug, OSName: config.OSName, OSVersion: config.OSVersion,
		Language: config.Language, OSArchitecture: config.OSArchitecture,
	}
	if !config.ID.IsNull() {
		id := config.ID.ValueString()
		for i := range images {
			if images[i].ID == id {
				return &images[i], nil
			}
		}
		diags.AddError("No image found", fmt.Sprintf("No public image has id %s.", id))
		return nil, diags
	}
	var matches []client.Image
	for i := range images {
		if filter.matches(&images[i]) {
			matches = append(matches, images[i])
		}
	}
	if len(matches) > 1 && config.MostRecent.ValueBool() {
		sort.SliceStable(matches, func(i, j int) bool { return matches[i].Created > matches[j].Created })
		return &matches[0], nil
	}
	switch len(matches) {
	case 1:
		return &matches[0], nil
	case 0:
		diags.AddError("No image found", fmt.Sprintf("No active image matches %s.", filter.describe()))
	default:
		ids := make([]string, 0, len(matches))
		for i := range matches {
			ids = append(ids, matches[i].ID+" ("+matches[i].Slug+")")
		}
		diags.AddError("Several images found", fmt.Sprintf("%d images match %s: %s. Narrow the lookup with language or "+
			"os_architecture, look it up by id or slug, or set most_recent = true.", len(matches), filter.describe(), strings.Join(ids, ", ")))
	}
	return nil, diags
}

// NewImagesDataSource returns the onetsolutions_images data source.
func NewImagesDataSource() datasource.DataSource {
	return &imagesDataSource{}
}

type imagesDataSource struct {
	common.DataSourceBase
}

type imagesModel struct {
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	OSName         types.String `tfsdk:"os_name"`
	OSVersion      types.String `tfsdk:"os_version"`
	Language       types.String `tfsdk:"language"`
	OSArchitecture types.String `tfsdk:"os_architecture"`
	Visibility     types.String `tfsdk:"visibility"`
	Images         types.List   `tfsdk:"images"`
}

func (d *imagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_images"
}

func (d *imagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the active images, optionally filtered, most recently published first.",
		Attributes: common.MergeAttributes(filterAttributes(false), common.DataSourceScopeAttributes(), map[string]schema.Attribute{
			"images": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The matching images.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: common.MergeAttributes(imageComputedAttributes(), map[string]schema.Attribute{
						"id":              schema.StringAttribute{Computed: true, MarkdownDescription: "The UUID of the image."},
						"slug":            schema.StringAttribute{Computed: true, MarkdownDescription: "The slug of the image."},
						"visibility":      schema.StringAttribute{Computed: true, MarkdownDescription: "`public` or `private`."},
						"os_name":         schema.StringAttribute{Computed: true, MarkdownDescription: "The operating system."},
						"os_version":      schema.StringAttribute{Computed: true, MarkdownDescription: "The version of the operating system."},
						"language":        schema.StringAttribute{Computed: true, MarkdownDescription: "The language of the system."},
						"os_architecture": schema.StringAttribute{Computed: true, MarkdownDescription: "The CPU architecture."},
					}),
				},
			},
		}),
	}
}

func (d *imagesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config imagesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	images, scope, diags := listImages(ctx, &d.DataSourceBase, c, config.Visibility.ValueString(), config.OrganizationID, config.ProjectID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	filter := imageFilter{
		Slug: types.StringNull(), OSName: config.OSName, OSVersion: config.OSVersion,
		Language: config.Language, OSArchitecture: config.OSArchitecture,
	}
	models := make([]imageModel, 0, len(images))
	for i := range images {
		if filter.matches(&images[i]) {
			models = append(models, newImageModel(&images[i]))
		}
	}
	sort.SliceStable(models, func(i, j int) bool { return models[i].Created.ValueString() > models[j].Created.ValueString() })
	list, listDiags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: imageAttrTypes}, models)
	resp.Diagnostics.Append(listDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	config.Images = list
	config.OrganizationID, config.ProjectID = scopeValues(scope, config.OrganizationID, config.ProjectID)
	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
