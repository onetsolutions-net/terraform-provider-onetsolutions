// Package provider defines the onetsolutions provider: its settings, the client it builds and
// the registry of every resource, data source and action.
package provider

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

// Option tunes the provider for tests: polling and retry intervals, transport.
type Option func(*options)

type options struct {
	waiter       waiter.Config
	retryWaitMin time.Duration
	retryWaitMax time.Duration
	transport    http.RoundTripper
}

// WithWaiter sets the polling intervals of every wait.
func WithWaiter(cfg waiter.Config) Option {
	return func(o *options) { o.waiter = cfg }
}

// WithRetryWait sets the bounds of the client's retry backoff.
func WithRetryWait(minWait, maxWait time.Duration) Option {
	return func(o *options) { o.retryWaitMin, o.retryWaitMax = minWait, maxWait }
}

// WithTransport replaces the HTTP transport of the client.
func WithTransport(transport http.RoundTripper) Option {
	return func(o *options) { o.transport = transport }
}

type onetsolutionsProvider struct {
	version string
	options options
}

var (
	_ provider.Provider            = (*onetsolutionsProvider)(nil)
	_ provider.ProviderWithActions = (*onetsolutionsProvider)(nil)
)

// New returns the provider factory providerserver serves.
func New(version string, opts ...Option) func() provider.Provider {
	return func() provider.Provider {
		p := &onetsolutionsProvider{version: version}
		for _, opt := range opts {
			opt(&p.options)
		}
		return p
	}
}

type providerModel struct {
	APIKey               types.String `tfsdk:"api_key"`
	APIURL               types.String `tfsdk:"api_url"`
	OrganizationID       types.String `tfsdk:"organization_id"`
	ProjectID            types.String `tfsdk:"project_id"`
	MaxRequestsPerSecond types.Int64  `tfsdk:"max_requests_per_second"`
}

func (p *onetsolutionsProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "onetsolutions"
	resp.Version = p.version
}

func (p *onetsolutionsProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages OnetSolutions compute instances, private networks, DNS and domains. Every " +
			"setting can also come from an environment variable.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "An API key created in the OnetSolutions console (`sk-onetsolutions-…`). " +
					"Required, here or through `" + common.EnvAPIKey + "`.",
			},
			"api_url": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The API endpoint. Defaults to `" + client.DefaultBaseURL + "`. " +
					"Can also be set with the `" + common.EnvAPIURL + "` environment variable. Must use https, except for " +
					"`localhost`; the provider never follows a redirect, so the key is sent to this URL only.",
				Validators: []validator.String{urlValidator{}},
			},
			"organization_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The organization resources are created in when they do not set one. " +
					"Can also be set with the `" + common.EnvOrganizationID + "` environment variable. " +
					"May be omitted when the API key's user belongs to exactly one organization.",
				Validators: []validator.String{common.UUID()},
			},
			"project_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The project resources are created in when they do not set one. " +
					"Can also be set with the `" + common.EnvProjectID + "` environment variable. " +
					"May be omitted when the organization has exactly one project.",
				Validators: []validator.String{common.UUID()},
			},
			"max_requests_per_second": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: fmt.Sprintf("The most API requests the provider sends per second, across all "+
					"resources. Defaults to %d. Can also be set with the `%s` environment variable.",
					int(client.DefaultRequestsPerSecond), common.EnvMaxRequestsPerSecond),
				Validators: []validator.Int64{int64validator.Between(1, 100)},
			},
		},
	}
}

func (p *onetsolutionsProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if unknown := unknownSettings(config); len(unknown) > 0 {
		if req.ClientCapabilities.DeferralAllowed {
			resp.Deferred = &provider.Deferred{Reason: provider.DeferredReasonProviderConfigUnknown}
			return
		}
		data := &common.ProviderData{NotReady: fmt.Sprintf(
			"The provider settings %s depend on values known only after apply, so the provider cannot call "+
				"the API during this plan. Apply the resources they depend on first (terraform apply -target=…).",
			strings.Join(unknown, ", "))}
		resp.ResourceData, resp.DataSourceData, resp.ActionData = data, data, data
		return
	}

	apiKey := settingOrEnv(config.APIKey, common.EnvAPIKey)
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(path.Root("api_key"), "Missing API key",
			"Create an API key in the OnetSolutions console and set it as api_key in the provider block "+
				"or in the "+common.EnvAPIKey+" environment variable.")
	}
	org := settingOrEnv(config.OrganizationID, common.EnvOrganizationID)
	project := settingOrEnv(config.ProjectID, common.EnvProjectID)
	for name, value := range map[string]string{"organization_id": org, "project_id": project} {
		if value != "" && !common.IsUUID(value) {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Invalid "+name,
				fmt.Sprintf("%q is not a UUID (from the provider block or its environment variable).", value))
		}
	}
	rps, err := requestsPerSecond(config.MaxRequestsPerSecond)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("max_requests_per_second"), "Invalid max_requests_per_second", err.Error())
	}
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient, err := client.New(client.Config{
		BaseURL:           settingOrEnv(config.APIURL, common.EnvAPIURL),
		APIKey:            apiKey,
		UserAgent:         client.UserAgent(p.version, req.TerraformVersion),
		RequestsPerSecond: rps,
		Burst:             max(client.DefaultBurst, int(rps)),
		RetryWaitMin:      p.options.retryWaitMin,
		RetryWaitMax:      p.options.retryWaitMax,
		Transport:         p.options.transport,
	})
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("api_url"), "Invalid api_url", err.Error())
		return
	}
	tflog.Debug(client.MaskedContext(ctx), "provider configured", map[string]any{
		"organization_id": org, "project_id": project, "max_requests_per_second": rps,
	})

	data := &common.ProviderData{
		Client:                apiClient,
		DefaultOrganizationID: org,
		DefaultProjectID:      project,
		Waiter:                p.options.waiter,
	}
	resp.ResourceData, resp.DataSourceData, resp.ActionData = data, data, data
}

func (p *onetsolutionsProvider) Resources(context.Context) []func() resource.Resource {
	return allResources()
}

func (p *onetsolutionsProvider) DataSources(context.Context) []func() datasource.DataSource {
	return allDataSources()
}

// Actions are invoked by Terraform 1.14 and later; other clients ignore them.
func (p *onetsolutionsProvider) Actions(context.Context) []func() action.Action {
	return allActions()
}

func unknownSettings(config providerModel) []string {
	var unknown []string
	for name, value := range map[string]interface{ IsUnknown() bool }{
		"api_key":                 config.APIKey,
		"api_url":                 config.APIURL,
		"organization_id":         config.OrganizationID,
		"project_id":              config.ProjectID,
		"max_requests_per_second": config.MaxRequestsPerSecond,
	} {
		if value.IsUnknown() {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	return unknown
}

func settingOrEnv(value types.String, env string) string {
	if !value.IsNull() && !value.IsUnknown() {
		return value.ValueString()
	}
	return os.Getenv(env)
}

func requestsPerSecond(value types.Int64) (float64, error) {
	if !value.IsNull() && !value.IsUnknown() {
		return float64(value.ValueInt64()), nil
	}
	raw := os.Getenv(common.EnvMaxRequestsPerSecond)
	if raw == "" {
		return client.DefaultRequestsPerSecond, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 || parsed > 100 {
		return 0, fmt.Errorf("%s=%q must be an integer between 1 and 100", common.EnvMaxRequestsPerSecond, raw)
	}
	return float64(parsed), nil
}

type urlValidator struct{}

func (urlValidator) Description(context.Context) string {
	return "value must be an absolute https:// URL (http:// only for localhost)"
}

func (v urlValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (urlValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := client.ParseBaseURL(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid URL", err.Error()+".")
	}
}
