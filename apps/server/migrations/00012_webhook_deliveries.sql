-- A webhook's recent deliveries, so the webhooks page can show whether its endpoint is receiving them and resend one.
--
-- Until now the outcome of a delivery was only logged: a webhook whose endpoint failed on every event still looked healthy, and an event lost after its retries could not be sent again. Each row is one event delivered to one webhook, one Standard Webhooks message (message_id is its webhook-id header), updated by every attempt: the webhook.deliver job's first attempt and retries, and a resend from the webhooks page. The server keeps the newest rows of each webhook and deletes older ones as it adds a row (webhook.deliveriesKept).
--
-- status is the message's state after its latest attempt: succeeded, retrying (the job will try again at next_attempt_at) or failed (its retries ran out, or a resend failed after they had). failure is why the latest attempt failed: an answer outside 2xx (status_code holds it), a redirect, no answer in time, no connection, or a URL the SSRF guard refused; failure_message is the reason as the page shows it, never the URL's path or query.
--
-- payload is the event's data as it was sent, kept so that a resend sends the same body. It is the response's data, so the row goes with the response, and with its survey, whose deletion deletes its responses: an erased response is never sent again and does not stay here.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. The foreign keys take a SHARE ROW EXCLUSIVE lock on webhooks and responses, and a migration should fail fast on a lock wait rather than queue every later write behind it.
SET LOCAL lock_timeout = '1s';

CREATE TABLE webhook_deliveries (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    webhook_id uuid NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
    response_id uuid NOT NULL REFERENCES responses (id) ON DELETE CASCADE,
    survey_id uuid NOT NULL,
    event text NOT NULL CHECK (event IN ('responseFinished', 'responseCreated', 'responseUpdated')),
    message_id text NOT NULL,
    payload text NOT NULL,
    status text NOT NULL CHECK (status IN ('succeeded', 'retrying', 'failed')),
    attempts integer NOT NULL CHECK (attempts >= 1),
    last_attempted_at timestamptz NOT NULL,
    first_failed_at timestamptz,
    next_attempt_at timestamptz,
    status_code integer CHECK (status_code BETWEEN 100 AND 999),
    failure text CHECK (failure IN ('status', 'redirect', 'timeout', 'unreachable', 'refused')),
    failure_message text,
    latency_ms integer NOT NULL CHECK (latency_ms >= 0),
    CHECK ((status = 'retrying') = (next_attempt_at IS NOT NULL)),
    CHECK ((status = 'succeeded') = (failure IS NULL)),
    CHECK ((failure IS NULL) = (failure_message IS NULL)),
    UNIQUE (webhook_id, message_id)
);
CREATE INDEX webhook_deliveries_webhook_id_created_at_idx ON webhook_deliveries (webhook_id, created_at DESC, id DESC);
CREATE INDEX webhook_deliveries_response_id_idx ON webhook_deliveries (response_id);

-- +goose Down

SET LOCAL lock_timeout = '1s';

DROP TABLE webhook_deliveries;
