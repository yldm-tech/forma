-- The organization's "Allow AI to read responses" switch (owner decision D1). AI smart tools (is_ai_smart_tools_enabled) were once described as never touching the data an organization collects; the open-text summary sends respondents' answers, with email addresses and phone numbers removed, to the configured AI provider, so it needs this second switch on as well.
--
-- A new organization starts with it on: its owners turn AI on under copy that says what is sent. An organization that already had AI smart tools on turned them on under the old promise, so it starts with the switch off until an owner or manager turns it on. One that had AI off starts with it on, and turns AI on, if ever, under the new copy.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. Adding a column with a constant default is a catalog change, but the ACCESS EXCLUSIVE lock it needs waits behind any open reader of organizations, which most authorization decisions read, and a migration should fail fast rather than queue every later query behind it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE organizations ADD COLUMN is_ai_response_reading_enabled boolean NOT NULL DEFAULT true;

UPDATE organizations SET is_ai_response_reading_enabled = false WHERE is_ai_smart_tools_enabled;

-- +goose Down

SET LOCAL lock_timeout = '1s';

ALTER TABLE organizations DROP COLUMN is_ai_response_reading_enabled;
