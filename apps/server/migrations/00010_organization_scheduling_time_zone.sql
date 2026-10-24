-- An organization's own time zone for scheduled surveys.
--
-- A survey's publish and close dates are calendar days: the server snaps each to the configured local time (SURVEY_SCHEDULING_LOCAL_HOUR and _MINUTE) of that day in a time zone, which until now was the instance's SURVEY_SCHEDULING_TIME_ZONE for every organization. survey_scheduling_time_zone is the IANA zone an organization's surveys use instead; null keeps the instance's. The server validates the name, as it does display_time_zone.
--
-- organizations is a table the forma2 import writes, and `forma import-forma2 --refresh` refuses when its digest (forma2import.targetDigests: the row count and the sum of a hash of each row's text) differs from the one the latest import or discard recorded. A new column changes every row's text without changing any data, so a recorded digest that still matches the table as it was is carried over to the new row shape, and a refresh after this migration is not refused for it. A digest that no longer matched (the organizations were changed here since the record) is left as it is.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. Adding a nullable column without a default is a catalog change, but the ACCESS EXCLUSIVE lock it needs waits behind any open reader of organizations, and a migration should fail fast rather than queue every later query behind it.
SET LOCAL lock_timeout = '1s';
-- The settings targetDigests takes its digests under, which shape a row's text.
SET LOCAL TimeZone = 'UTC';
SET LOCAL DateStyle = 'ISO, YMD';
SET LOCAL IntervalStyle = 'postgres';
SET LOCAL extra_float_digits = 1;
SET LOCAL bytea_output = 'hex';

-- +goose StatementBegin
DO $$
DECLARE
  before_digest text;
  after_digest text;
BEGIN
  SELECT count(*)::text || ':' || COALESCE(sum(hashtextextended(t::text, 0)), 0)::text INTO before_digest FROM organizations t;
  ALTER TABLE organizations ADD COLUMN survey_scheduling_time_zone text;
  SELECT count(*)::text || ':' || COALESCE(sum(hashtextextended(t::text, 0)), 0)::text INTO after_digest FROM organizations t;
  UPDATE forma2_imports SET target_digests = jsonb_set(target_digests, '{organizations}', to_jsonb(after_digest))
  WHERE target_digests->>'organizations' = before_digest;
END $$;
-- +goose StatementEnd

-- +goose Down

SET LOCAL lock_timeout = '1s';
SET LOCAL TimeZone = 'UTC';
SET LOCAL DateStyle = 'ISO, YMD';
SET LOCAL IntervalStyle = 'postgres';
SET LOCAL extra_float_digits = 1;
SET LOCAL bytea_output = 'hex';

-- +goose StatementBegin
DO $$
DECLARE
  before_digest text;
  after_digest text;
BEGIN
  SELECT count(*)::text || ':' || COALESCE(sum(hashtextextended(t::text, 0)), 0)::text INTO before_digest FROM organizations t;
  ALTER TABLE organizations DROP COLUMN survey_scheduling_time_zone;
  SELECT count(*)::text || ':' || COALESCE(sum(hashtextextended(t::text, 0)), 0)::text INTO after_digest FROM organizations t;
  UPDATE forma2_imports SET target_digests = jsonb_set(target_digests, '{organizations}', to_jsonb(after_digest))
  WHERE target_digests->>'organizations' = before_digest;
END $$;
-- +goose StatementEnd
