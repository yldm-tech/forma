package billing

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
)

// The organization lifecycle queues forma2's setup and cleanup as jobs on Forma Cloud with Stripe, and nothing anywhere else.
func TestLifecycle(t *testing.T) {
	for name, tc := range map[string]struct {
		configure func(*config.Config)
		jobs      int
	}{
		"cloud":         {func(*config.Config) {}, 2},
		"self-hosted":   {func(c *config.Config) { c.IsFormaCloud = false }, 0},
		"no Stripe key": {func(c *config.Config) { c.Stripe.SecretKey = "" }, 0},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, tc.configure)
			enqueuer, err := jobs.NewEnqueuer(e.log)
			if err != nil {
				t.Fatal(err)
			}
			l := NewLifecycle(e.cfg, e.engine.stripe, e.db, enqueuer, e.log)
			org := uuid.New()
			customer := "cus_123"
			l.OrganizationCreated(context.Background(), org)
			l.OrganizationDeleted(context.Background(), &customer)
			l.OrganizationDeleted(context.Background(), nil)
			if n := e.count(`SELECT count(*) FROM river_job WHERE (kind = 'billing.setup-organization' AND args ->> 'organizationId' = ?) OR (kind = 'billing.cleanup-customer' AND args ->> 'stripeCustomerId' = 'cus_123')`, org.String()); n != tc.jobs {
				t.Fatalf("%d jobs, want %d", n, tc.jobs)
			}
		})
	}
}
