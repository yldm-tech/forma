-- An organization's own time zone for scheduled surveys.
--
-- A survey's publish and close dates are calendar days: the server snaps each to the configured local time (SURVEY_SCHEDULING_LOCAL_HOUR and _MINUTE) of that day in a time zone, which until now was the instance's SURVEY_SCHEDULING_TIME_ZONE for every organization. survey_scheduling_time_zone is the IANA zone an organization's surveys use instead; null keeps the instance's. The server validates the name, as it does display_time_zone.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. Adding a nullable column without a default is a catalog change, but the ACCESS EXCLUSIVE lock it needs waits behind any open reader of organizations, and a migration should fail fast rather than queue every later query behind it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE organizations ADD COLUMN survey_scheduling_time_zone text;

-- +goose Down

ALTER TABLE organizations DROP COLUMN survey_scheduling_time_zone;
