package integration

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yldm-tech/forma/apps/server/internal/pipeline"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/responsemail"
)

// Deliverer writes finished responses to the workspace's integrations, forma2's handleIntegrations. It runs in `forma worker` as the response pipeline's integration hand-off, on behalf of no caller.
type Deliverer struct {
	repo    *Repository
	p       *providers
	tokens  *tokens
	files   fileURLs
	log     *slog.Logger
	airtime airtableTiming
}

var _ pipeline.IntegrationHandler = (*Deliverer)(nil)

func NewDeliverer(cfg config.Config, repo *Repository, rdb *redis.Client, log *slog.Logger) *Deliverer {
	p := newProviders(cfg)
	return &Deliverer{
		repo: repo, p: p, files: newFileURLs(cfg), log: log, airtime: defaultAirtableTiming,
		tokens: &tokens{repo: repo, rdb: rdb, p: p, log: log, now: time.Now, refresh: defaultAirtableRefreshTiming},
	}
}

// labels of the failure log lines, forma2's dispatch labels.
var deliveryLabels = map[string]string{TypeGoogleSheets: "google sheets", TypeSlack: "slack", TypeAirtable: "airtable", TypeNotion: "notion"}

// HandleResponse writes the response to every integration of its workspace that links its survey. The integrations run concurrently, each writing its links one after another in their stored order; a failing integration is logged as forma2 logged it and stops neither the others nor the pipeline. Only a failure to load the integrations is returned.
func (d *Deliverer) HandleResponse(ctx context.Context, ev *pipeline.Event) error {
	integrations, err := d.repo.OfWorkspace(ctx, ev.WorkspaceID)
	if err != nil {
		return err
	}
	if len(integrations) == 0 {
		return nil
	}
	answers, err := responsemail.Decode(&ev.Response)
	if err != nil {
		return err
	}
	r, err := newSubmission(answers, ev.Survey.Survey, d.files, ev.DisplayTimeZone)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, in := range integrations {
		label, ok := deliveryLabels[in.Type]
		if !ok {
			continue
		}
		wg.Go(func() {
			if err := d.deliver(ctx, in, r); err != nil {
				d.log.ErrorContext(ctx, "Error in "+label+" integration", "err", err, "integration_id", in.ID, "response_id", ev.Response.ID)
			}
		})
	}
	wg.Wait()
	return nil
}

// forSurvey reports whether a link is for the response's survey; links made in forma2 name it by its old id.
func forSurvey(l LinkBase, r *submission) bool {
	id, ok := legacyid.Resolve(l.SurveyID)
	return ok && id == r.answers.Response.SurveyID
}

func (d *Deliverer) deliver(ctx context.Context, in *Integration, r *submission) error {
	switch in.Type {
	case TypeGoogleSheets:
		return d.sheets(ctx, in, r)
	case TypeAirtable:
		return d.airtable(ctx, in, r)
	case TypeSlack:
		return d.slack(ctx, in, r)
	case TypeNotion:
		return d.notion(ctx, in, r)
	}
	return nil
}

// linkOf is a stored link of any type.
type linkOf interface {
	base() LinkBase
}

// forLinks calls write with each of the integration's links for the response's survey, in their stored order, stopping at the first failure as forma2 did.
func forLinks[T linkOf](in *Integration, r *submission, write func(link T) error) error {
	links, err := decodeLinks[T](in.Config)
	if err != nil {
		return fmt.Errorf("decode %s links: %w", in.Type, err)
	}
	for _, l := range links {
		if !forSurvey(l.base(), r) {
			continue
		}
		if err := write(l); err != nil {
			return err
		}
	}
	return nil
}

// sheets authorizes once, for the first link of the survey.
func (d *Deliverer) sheets(ctx context.Context, in *Integration, r *submission) error {
	token := ""
	return forLinks(in, r, func(l SheetLink) error {
		if token == "" {
			var err error
			if token, err = d.tokens.google(ctx, in); err != nil {
				return err
			}
		}
		row, err := r.build(selectionOf(l.LinkBase, l.Elements), false)
		if err != nil {
			return err
		}
		return d.p.writeSheet(ctx, token, l.SpreadsheetID, row.values, row.labels)
	})
}

// airtable resolves the credential once, and only when some link is for the response's survey, so a response to an unrelated survey never spends a refresh.
func (d *Deliverer) airtable(ctx context.Context, in *Integration, r *submission) error {
	var k *AirtableKey
	return forLinks(in, r, func(l AirtableLink) error {
		if k == nil {
			var err error
			if k, err = d.tokens.airtable(ctx, in); err != nil {
				return err
			}
		}
		row, err := r.build(selectionOf(l.LinkBase, l.Elements), false)
		if err != nil {
			return err
		}
		return d.p.writeAirtable(ctx, d.airtime, k.AccessToken, l, row.values, row.labels)
	})
}

func (d *Deliverer) slack(ctx context.Context, in *Integration, r *submission) error {
	return forLinks(in, r, func(l SlackLink) error {
		k, ok := key[SlackKey](in)
		if !ok {
			return fmt.Errorf("slack integration %s has no credential", in.ID)
		}
		row, err := r.build(selectionOf(l.LinkBase, l.Elements), true)
		if err != nil {
			return err
		}
		return d.p.writeSlack(ctx, k.AccessToken, l.ChannelID, r.survey.Name, row.values, row.labels)
	})
}

func (d *Deliverer) notion(ctx context.Context, in *Integration, r *submission) error {
	return forLinks(in, r, func(l NotionLink) error {
		k, ok := key[notionKey](in)
		if !ok {
			return fmt.Errorf("notion integration %s has no credential", in.ID)
		}
		return d.p.writeNotion(ctx, k.AccessToken, l.DatabaseID, r.notionProperties(l.Mapping))
	})
}
