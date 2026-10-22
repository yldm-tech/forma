//go:build wireinject

package app

import (
	"context"
	"log/slog"

	"github.com/google/wire"

	"github.com/yldm-tech/forma/server/internal/actionclass"
	"github.com/yldm-tech/forma/server/internal/analysis"
	"github.com/yldm-tech/forma/server/internal/apikey"
	"github.com/yldm-tech/forma/server/internal/auth"
	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/clientapi"
	"github.com/yldm-tech/forma/server/internal/contact"
	"github.com/yldm-tech/forma/server/internal/followup"
	"github.com/yldm-tech/forma/server/internal/health"
	"github.com/yldm-tech/forma/server/internal/invite"
	"github.com/yldm-tech/forma/server/internal/jsonschema"
	"github.com/yldm-tech/forma/server/internal/linksurvey"
	"github.com/yldm-tech/forma/server/internal/notification"
	"github.com/yldm-tech/forma/server/internal/org"
	"github.com/yldm-tech/forma/server/internal/pipeline"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/database"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/httpserver"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/platform/ssrf"
	surveyv3 "github.com/yldm-tech/forma/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/server/internal/platform/valkey"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/storage"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
	"github.com/yldm-tech/forma/server/internal/tag"
	"github.com/yldm-tech/forma/server/internal/team"
	"github.com/yldm-tech/forma/server/internal/v3api"
	"github.com/yldm-tech/forma/server/internal/webhook"
	"github.com/yldm-tech/forma/server/internal/webui"
	"github.com/yldm-tech/forma/server/internal/workspace"
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

// authSet is the identity module: sessions, sign-up, sign-in, two-factor authentication and the account flows under /api/app/auth, and single sign-on with its identity provider callbacks. Its sessions are the SessionAuth every other module's routes authenticate with.
var authSet = wire.NewSet(
	auth.NewRepository,
	auth.NewSessions,
	wire.Bind(new(httpapi.SessionAuth), new(*auth.Sessions)),
	auth.NewService,
	auth.NewTwoFactor,
	auth.NewHandler,
	auth.NewSSO,
	auth.NewSSOHandler,
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

// surveySet is the survey definition module: /api/app/surveys, /api/app/quotas and /api/v3/surveys, the latter authenticated by session or API key.
var surveySet = wire.NewSet(
	surveydoc.NewValidator,
	survey.LoadCatalog,
	survey.NewRepository,
	survey.NewService,
	surveyv3.NewGuard,
	survey.NewHandler,
)

// v3Set is the /api/v3 conventions of action classes and tags: authentication by session or API key, the api:v3 rate limit and audit events.
var v3Set = wire.NewSet(
	v3api.NewMiddleware,
	v3api.NewAuditor,
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

// responseSet is response collection and analysis: the steps the client API drives, the response views' routes under /api/app, the summary, filter options, export and reset of the analysis views, and the job enqueuer their writes share their transaction with.
var responseSet = wire.NewSet(
	jobs.NewEnqueuer,
	response.NewService,
	response.NewHandler,
	analysis.NewService,
	analysis.NewHandler,
)

// storageSet is file storage: the S3 bucket, the layout of the files in it, and the /storage route that serves and deletes them. Deleting a workspace deletes its files through it.
var storageSet = wire.NewSet(
	storage.NewBucket,
	storage.NewService,
	wire.Bind(new(workspace.FileStore), new(*storage.Service)),
	storage.NewHandler,
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

// pipelineSet is the response pipeline `forma worker` runs after every response write: webhook delivery, follow-up emails, notification emails and survey auto-complete. Integrations and workflows plug in through pipeline.Handoffs, empty until those modules land.
var pipelineSet = wire.NewSet(
	valkey.Open,
	ratelimit.New,
	mailer.New,
	ssrf.New,
	webhook.NewRepository,
	webhook.NewDeliverer,
	followup.NewSender,
	notification.NewNotifier,
	wire.Value(pipeline.Handoffs{}),
	pipeline.NewWorker,
)

// InitializeServer builds `forma serve`. The returned cleanup closes the database pool, the Valkey client and the SpiceDB connection.
func InitializeServer(ctx context.Context, cfg config.Config, log *slog.Logger) (*httpserver.Server, func(), error) {
	wire.Build(
		database.Open,
		authz.OpenSpiceDB,
		authz.NewAuthorizer,
		platformSet,
		authSet,
		jsonschema.New,
		tenancySet,
		teamSet,
		inviteSet,
		apikeySet,
		surveySet,
		contactSet,
		v3Set,
		surveyConfigSet,
		responseSet,
		storageSet,
		linkSurveySet,
		clientAPISet,
		webhookSet,
		notification.NewHandler,
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
		NewJobRegistry,
		jobs.NewWorker,
	)
	return nil, nil, nil
}
