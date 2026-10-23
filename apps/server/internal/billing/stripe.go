package billing

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/stripe/stripe-go/v84"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Stripe is the instance's Stripe account: the API client, pinned to forma2's STRIPE_API_VERSION (2026-02-25.clover, the version stripe-go v84 is generated for), its price catalog, and the secrets of the webhook and of Stripe.js. The client is nil when STRIPE_SECRET_KEY is unset.
type Stripe struct {
	api            *stripe.Client
	webhookSecret  string
	publishableKey string
	catalog        *catalogCache
	log            *slog.Logger
	now            func() time.Time
}

// NewStripe opens the Stripe client when STRIPE_SECRET_KEY is set.
func NewStripe(cfg config.Config, log *slog.Logger) *Stripe {
	return newStripe(cfg, nil, log)
}

// newStripe builds the client on the given backends; nil means Stripe's own API.
func newStripe(cfg config.Config, backends *stripe.Backends, log *slog.Logger) *Stripe {
	s := &Stripe{webhookSecret: string(cfg.Stripe.WebhookSecret), publishableKey: cfg.Stripe.PublishableKey, catalog: &catalogCache{}, log: log, now: time.Now}
	if cfg.Stripe.SecretKey != "" {
		var opts []stripe.ClientOption
		if backends != nil {
			opts = append(opts, stripe.WithBackends(backends))
		}
		s.api = stripe.NewClient(string(cfg.Stripe.SecretKey), opts...)
	}
	return s
}

// Configured reports whether the Stripe client exists.
func (s *Stripe) Configured() bool { return s.api != nil }

// stripeError is the Stripe API error in err's chain.
func stripeError(err error) (*stripe.Error, bool) {
	var se *stripe.Error
	ok := errors.As(err, &se)
	return se, ok
}

// isResourceMissing is a 404 resource_missing, an object that no longer exists.
func isResourceMissing(err error) bool {
	se, ok := stripeError(err)
	return ok && se.HTTPStatusCode == http.StatusNotFound && se.Code == stripe.ErrorCodeResourceMissing
}
