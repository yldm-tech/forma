package digest

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
)

// scanInterval is how often the worker looks for organizations whose summary is due. Every UTC offset in use is a whole number of quarter hours, so a scan every quarter hour reaches each zone's Monday 09:00 within that quarter hour.
const scanInterval = 15 * time.Minute

// unique makes a job with the same arguments a no-op while one is queued, running or completed. River keeps a completed job for a day (jobs.completedRetention), longer than the Monday from 09:00 on during which the scans enqueue a week's jobs again, so each organization's summary is fanned out once and each member's is sent once.
var unique = &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}

// recipientAttempts is how often a member's summary is tried before it is given up. The organization's job does not run again once it has fanned out, so a recipient job that runs out of attempts loses that member's week; with the worker's backoff (5 s doubling) ten attempts span about an hour and a half, which rides out a mail server that is down or refusing for a while.
const recipientAttempts = 10

var uniqueRecipient = &river.InsertOpts{MaxAttempts: recipientAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true}}

// ScanArgs is the periodic job that enqueues an OrganizationArgs job for every organization whose summary is due.
type ScanArgs struct{}

func (ScanArgs) Kind() string { return "notification.weekly-summary-scan" }

// OrganizationArgs fans an organization's summary for a week out to its recipients. Week is the date of the summarized week's Monday in the organization's zone.
type OrganizationArgs struct {
	OrganizationID uuid.UUID `json:"organizationId"`
	Week           string    `json:"week"`
}

func (OrganizationArgs) Kind() string { return "notification.weekly-summary-organization" }

// RecipientArgs sends one member the organization's summary for the week.
type RecipientArgs struct {
	OrganizationID uuid.UUID `json:"organizationId"`
	UserID         uuid.UUID `json:"userId"`
	Week           string    `json:"week"`
}

func (RecipientArgs) Kind() string { return "notification.weekly-summary-recipient" }

type scanWorker struct {
	river.WorkerDefaults[ScanArgs]
	digest   *Digest
	enqueuer *jobs.Enqueuer
}

// Work enqueues the due organizations' jobs. Without a mail server there is nobody to send to, so it reads nothing.
func (w *scanWorker) Work(ctx context.Context, _ *river.Job[ScanArgs]) error {
	if !w.digest.mailer.Configured() {
		return nil
	}
	due, err := w.digest.DueOrganizations(ctx, time.Now())
	if err != nil || len(due) == 0 {
		return err
	}
	return w.enqueuer.Transaction(ctx, w.digest.db, func(tx *jobs.Tx) error {
		for _, args := range due {
			if err := tx.Enqueue(ctx, args, unique); err != nil {
				return err
			}
		}
		return nil
	})
}

type organizationWorker struct {
	river.WorkerDefaults[OrganizationArgs]
	digest   *Digest
	enqueuer *jobs.Enqueuer
}

// Work enqueues a RecipientArgs job for each member who gets the summary.
func (w *organizationWorker) Work(ctx context.Context, job *river.Job[OrganizationArgs]) error {
	recipients, err := w.digest.Recipients(ctx, job.Args.OrganizationID, nil)
	if err != nil || len(recipients) == 0 {
		return err
	}
	return w.enqueuer.Transaction(ctx, w.digest.db, func(tx *jobs.Tx) error {
		for _, r := range recipients {
			if err := tx.Enqueue(ctx, RecipientArgs{OrganizationID: job.Args.OrganizationID, UserID: r.UserID, Week: job.Args.Week}, uniqueRecipient); err != nil {
				return err
			}
		}
		return nil
	})
}

type recipientWorker struct {
	river.WorkerDefaults[RecipientArgs]
	digest *Digest
	log    *slog.Logger
}

// Work sends the member's summary. A failed delivery fails the job, so River retries that one member alone.
func (w *recipientWorker) Work(ctx context.Context, job *river.Job[RecipientArgs]) error {
	sent, err := w.digest.Send(ctx, job.Args.OrganizationID, job.Args.UserID, job.Args.Week)
	if err != nil {
		return err
	}
	w.log.InfoContext(ctx, "Weekly summary processed", "organizationId", job.Args.OrganizationID.String(), "userId", job.Args.UserID.String(), "week", job.Args.Week, "sent", sent)
	return nil
}

// RegisterJobs adds the weekly summary to `forma worker`: a scan every quarter hour, from the worker's start, that fans each due organization out to one job per recipient.
func RegisterJobs(reg *jobs.Registry, digest *Digest, enqueuer *jobs.Enqueuer, log *slog.Logger) {
	jobs.Add(reg, &scanWorker{digest: digest, enqueuer: enqueuer})
	jobs.Add(reg, &organizationWorker{digest: digest, enqueuer: enqueuer})
	jobs.Add(reg, &recipientWorker{digest: digest, log: log})
	reg.AddPeriodic(river.NewPeriodicJob(river.PeriodicInterval(scanInterval), func() (river.JobArgs, *river.InsertOpts) {
		return ScanArgs{}, nil
	}, &river.PeriodicJobOpts{ID: "notification-weekly-summary-scan", RunOnStart: true}))
}
