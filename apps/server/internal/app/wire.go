//go:build wireinject

package app

import (
	"context"
	"log/slog"

	"github.com/google/wire"

	"github.com/yldm-tech/forma/apps/server/internal/actionclass"
	"github.com/yldm-tech/forma/apps/server/internal/ai"
	"github.com/yldm-tech/forma/apps/server/internal/analysis"
	"github.com/yldm-tech/forma/apps/server/internal/apikey"
	"github.com/yldm-tech/forma/apps/server/internal/auth"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/billing"
	"github.com/yldm-tech/forma/apps/server/internal/brevo"
	"github.com/yldm-tech/forma/apps/server/internal/clientapi"
	"github.com/yldm-tech/forma/apps/server/internal/contact"
	"github.com/yldm-tech/forma/apps/server/internal/contact/contactsurvey"
	"github.com/yldm-tech/forma/apps/server/internal/followup"
	"github.com/yldm-tech/forma/apps/server/internal/health"
	"github.com/yldm-tech/forma/apps/server/internal/instance"
	"github.com/yldm-tech/forma/apps/server/internal/integration"
	"github.com/yldm-tech/forma/apps/server/internal/invite"
	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/linksurvey"
	"github.com/yldm-tech/forma/apps/server/internal/mcp"
	"github.com/yldm-tech/forma/apps/server/internal/mgmtapi"
	"github.com/yldm-tech/forma/apps/server/internal/mgmtapi/mgmtv1"
	"github.com/yldm-tech/forma/apps/server/internal/notification"
	"github.com/yldm-tech/forma/apps/server/internal/oauthserver"
	"github.com/yldm-tech/forma/apps/server/internal/org"
	"github.com/yldm-tech/forma/apps/server/internal/pipeline"
	"github.com/yldm-tech/forma/apps/server/internal/plain"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ssrf"
	surveyv3 "github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/storage"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
	"github.com/yldm-tech/forma/apps/server/internal/survey/emailembed"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
	"github.com/yldm-tech/forma/apps/server/internal/tag"
	"github.com/yldm-tech/forma/apps/server/internal/team"
	"github.com/yldm-tech/forma/apps/server/internal/unsplash"
	"github.com/yldm-tech/forma/apps/server/internal/usagereport"
	"github.com/yldm-tech/forma/apps/server/internal/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/v3api/discovery"
	"github.com/yldm-tech/forma/apps/server/internal/webhook"
	"github.com/yldm-tech/forma/apps/server/internal/webui"
	"github.com/yldm-tech/forma/apps/server/internal/workflow"
	"github.com/yldm-tech/forma/apps/server/internal/workspace"
)

// platformSet is the shared infrastructure of `forma serve`, bundled into Platform for the feature modules.
var platformSet = wire.NewSet(
	valkey.Open,
	ratelimit.New,
	mailer.New,
	audit.New,
	httpapi.NewClientIPResolver,
	secret.NewCipher,
	wire.Struct(new(Platform), "*"),
)

// authSet is the identity module: sessions, sign-up, sign-in, two-factor authentication and the account flows under /api/app/auth, and single sign-on with its identity provider callbacks and SAML's assertion consumer service, with the Brevo contact sync its account changes enqueue. Its sessions are the SessionAuth every other module's routes authenticate with.
var authSet = wire.NewSet(
	auth.NewRepository,
	auth.NewSessions,
	wire.Bind(new(httpapi.SessionAuth), new(*auth.Sessions)),
	wire.Bind(new(oauthserver.Sessions), new(*auth.Sessions)),
	auth.NewService,
	auth.NewTwoFactor,
	auth.NewHandler,
	newSSO,
	auth.NewSSOHandler,
	brevo.NewContacts,
)

// tenancySet is the organization and workspace modules under /api/app/organizations, /api/app/workspaces and /api/app/home. Their routes authenticate through the credential module's sessions.
var tenancySet = wire.NewSet(
	workspace.NewRepository,
	workspace.NewService,
	workspace.NewHandler,
	org.NewRepository,
	org.NewService,
	org.NewHandler,
)

// teamSet is teams, their members and their workspace access.
var teamSet = wire.NewSet(
	team.NewRepository,
	team.NewService,
	team.NewHandler,
)

// inviteSet is invites and their acceptance; sign-up redeems invites through it.
var inviteSet = wire.NewSet(
	invite.NewRepository,
	invite.NewService,
	wire.Bind(new(auth.InviteRedeemer), new(*invite.Service)),
	invite.NewHandler,
)

// apikeySet is the organization API key module under /api/app/organizations/:organizationId/api-keys, and the API key authentication of /api/v3.
var apikeySet = wire.NewSet(
	apikey.NewRepository,
	apikey.NewService,
	apikey.NewHandler,
	apikey.NewAuthenticator,
)

// contactSet is contacts, their attribute keys and values, CSV import and segments under /api/app, and /api/v3/contact-attribute-keys.
var contactSet = wire.NewSet(
	contact.NewRepository,
	contact.NewService,
	contact.NewHandler,
	contact.NewV3Handler,
)

// aiSet is the instance's AI provider, the organization AI service every AI feature generates through, and its /api/app routes: availability, the editor's AI translation and a survey summary's example responses.
var aiSet = wire.NewSet(
	ai.NewClient,
	ai.NewRepository,
	ai.NewService,
	ai.NewExampleResponses,
	ai.NewHandler,
)

// surveySet is the survey definition module: /api/app/surveys, /api/app/quotas and /api/v3/surveys, the latter authenticated by session or API key.
var surveySet = wire.NewSet(
	surveydoc.NewValidator,
	survey.LoadCatalog,
	survey.NewRepository,
	survey.NewService,
	surveyv3.NewGuard,
	survey.NewHandler,
)

// v3Set is the /api/v3 conventions of action classes and tags: authentication by session or API key, the api:v3 rate limit and audit events; and the v3 discovery routes, GET /api/v3/me and the OpenAPI document.
var v3Set = wire.NewSet(
	v3api.NewMiddleware,
	v3api.NewAuditor,
	discovery.NewHandler,
)

// surveyConfigSet is the workspace-level survey configuration: action classes and tags, under /api/app and /api/v3.
var surveyConfigSet = wire.NewSet(
	actionclass.NewRepository,
	actionclass.NewService,
	actionclass.NewHandler,
	tag.NewRepository,
	tag.NewService,
	tag.NewHandler,
)

// responseSet is response collection and analysis: the steps the client API drives, the response views' routes under /api/app, the summary, filter options, export, attachment export and reset of the analysis views, the job enqueuer their writes share their transaction with, and the quota screening the AI example responses are written through.
var responseSet = wire.NewSet(
	jobs.NewEnqueuer,
	response.NewService,
	wire.Bind(new(ai.QuotaScreener), new(*response.Service)),
	response.NewHandler,
	analysis.NewService,
	analysis.NewHandler,
	analysis.NewAttachmentExporter,
	analysis.NewAttachmentsHandler,
)

// storageSet is file storage: the S3 bucket, the layout of the files in it, and the /storage route that serves and deletes them. Deleting a workspace deletes its files through it.
var storageSet = wire.NewSet(
	storage.NewBucket,
	storage.NewService,
	wire.Bind(new(workspace.FileStore), new(*storage.Service)),
	storage.NewHandler,
)

// instanceSet is /api/app/instance: whether file storage is configured, the running release, and the latest stable release for the self-hosted update notice.
var instanceSet = wire.NewSet(
	instance.NewReleases,
	instance.NewHandler,
)

// linkSurveySet is the public survey pages (/s, /c, /p), their PIN and email verification gates, and the OG image.
var linkSurveySet = wire.NewSet(
	linksurvey.NewRepository,
	linksurvey.NewService,
	linksurvey.NewHandler,
)

// clientAPISet is the client API under /api/v1/client and /api/v2/client, with its reCAPTCHA verifier.
var clientAPISet = wire.NewSet(
	clientapi.NewRecaptcha,
	clientapi.NewHandler,
)

// webhookSet is webhooks: the settings page's routes under /api/app, the /api/v1/webhooks subscription API, and the SSRF guard every request to a webhook's URL goes through.
var webhookSet = wire.NewSet(
	ssrf.New,
	webhook.NewRepository,
	webhook.NewService,
	webhook.NewHandler,
	webhook.NewV1Handler,
)

// managementAPISet is forma2's API-key management APIs: the v1 wrapper the /api/v1 routes share (/api/v1/management and /api/v1/webhooks), and /api/v1/management with /api/v2.
var managementAPISet = wire.NewSet(
	mgmtv1.New,
	mgmtapi.NewHandler,
)

// integrationSet is the Google Sheets, Airtable, Notion and Slack integrations: the integrations pages' routes under /api/app, and forma2's OAuth authorize and callback routes at the paths registered with the providers.
var integrationSet = wire.NewSet(
	integration.NewRepository,
	integration.NewService,
	integration.NewHandler,
	integration.NewOAuthHandler,
)

// workflowSet is workflows: the /api/v3/workflows management API and the builder's authoring context under /api/app.
var workflowSet = wire.NewSet(
	workflow.NewRepository,
	workflow.NewService,
	workflow.NewHandler,
)

// mcpSet is the MCP server at /api/mcp and the OAuth authorization server MCP clients sign in through, under /api/auth/oauth2 with its consent page's routes under /api/app.
var mcpSet = wire.NewSet(
	oauthserver.NewKeys,
	oauthserver.NewHandler,
	oauthserver.NewAppHandler,
	oauthserver.NewTokenVerifier,
	mcp.NewHandler,
)

// billingSet is Forma Cloud billing: the Stripe account, the billing page's routes under /api/app, the Stripe webhook at forma2's /api/billing/stripe-webhook, and the jobs an organization's creation and deletion queue. It does nothing unless IS_FORMA_CLOUD=1 and STRIPE_SECRET_KEY are set.
var billingSet = wire.NewSet(
	billing.NewStripe,
	billing.NewRepository,
	billing.NewEngine,
	billing.NewService,
	billing.NewHandler,
	billing.NewWebhookHandler,
	billing.NewLifecycle,
)

// unsplashSet is the survey editor's Unsplash image search under /api/app/unsplash, offered when UNSPLASH_ACCESS_KEY is set.
var unsplashSet = wire.NewSet(
	unsplash.NewService,
	unsplash.NewHandler,
)

// contactSurveySet is a contact's activity on the contact page and personal links, one from the contact page or a CSV of a segment's from the share modal, under /api/app.
var contactSurveySet = wire.NewSet(
	contactsurvey.NewService,
	contactsurvey.NewHandler,
)

// emailEmbedSet is the share modal's Email tab under /api/app: a survey's email embed and the preview mailed to the member.
var emailEmbedSet = wire.NewSet(
	emailembed.NewService,
	emailembed.NewHandler,
)

// pipelineSet is the response pipeline `forma worker` runs after every response write: webhook delivery, integration delivery, workflow runs, follow-up emails, notification emails, survey auto-complete and the Stripe meter events of responses and workflow runs. Integrations, workflows and metering plug in through pipeline.Handoffs.
var pipelineSet = wire.NewSet(
	valkey.Open,
	ratelimit.New,
	mailer.New,
	ssrf.New,
	secret.NewCipher,
	webhook.NewRepository,
	webhook.NewDeliverer,
	followup.NewSender,
	notification.NewNotifier,
	integration.NewRepository,
	integration.NewDeliverer,
	jobs.NewEnqueuer,
	billing.NewStripe,
	billing.NewMeter,
	wire.Bind(new(pipeline.UsageMeter), new(*billing.Meter)),
	workflow.NewEnqueuer,
	workflow.NewRunner,
	workflow.NewReconciler,
	usagereport.NewReporter,
	newPipelineHandoffs,
	pipeline.NewWorker,
)

// InitializeServer builds `forma serve`. The returned cleanup closes the database pool, the Valkey client and the SpiceDB connection.
func InitializeServer(ctx context.Context, cfg config.Config, log *slog.Logger) (*httpserver.Server, func(), error) {
	wire.Build(
		database.Open,
		authz.OpenSpiceDB,
		authz.NewAuthorizer,
		// The API key module delivers a new key's relationships itself, before it returns the key.
		authz.NewDelivery,
		platformSet,
		authSet,
		jsonschema.New,
		tenancySet,
		teamSet,
		inviteSet,
		apikeySet,
		aiSet,
		surveySet,
		contactSet,
		v3Set,
		surveyConfigSet,
		responseSet,
		storageSet,
		instanceSet,
		linkSurveySet,
		clientAPISet,
		webhookSet,
		managementAPISet,
		integrationSet,
		workflowSet,
		mcpSet,
		billingSet,
		unsplashSet,
		notification.NewHandler,
		contactSurveySet,
		emailEmbedSet,
		plain.NewHandler,
		health.NewDatabasePinger,
		health.NewHandler,
		webui.New,
		NewRouter,
		httpserver.New,
	)
	return nil, nil, nil
}

// InitializeWorker builds `forma worker`. The returned cleanup closes the job queue's pool, the database pool, the Valkey client and the SpiceDB connection.
func InitializeWorker(ctx context.Context, cfg config.Config, log *slog.Logger) (*jobs.Worker, func(), error) {
	wire.Build(
		jobs.OpenPool,
		database.Open,
		authz.OpenSpiceDB,
		authz.NewDelivery,
		authz.NewAudit,
		audit.New,
		storage.NewBucket,
		storage.NewService,
		workspace.NewRepository,
		pipelineSet,
		billing.NewRepository,
		billing.NewEngine,
		brevo.NewClient,
		NewJobRegistry,
		jobs.NewWorker,
	)
	return nil, nil, nil
}
