-- What forma2_imports needs to notice changes after a recorded import or discard, on either side.
--
-- - target_digests: a digest per table of this schema the import writes, taken when the import or discard was recorded. `forma import-forma2 --refresh` compares them with the tables as they are, and refuses when this server has changed them since: a refresh would bring back rows deleted through this server (a revoked API key, a removed member) and overwrite what was changed, since it cannot tell those changes from forma2's.
-- - source_marks: forma2_marks() of forma2's tables as the import read them (or as they were when the discard was recorded). forma2_import_drift() compares them with forma2's tables as they are, so the startup guard and docker/forma.sh refuse a database where forma2 ran again after the record, as it does after a rollback: a server started on the earlier import would serve without what forma2 wrote since.
--
-- A record without them (written before this migration, or an import read from another database with --source) is not compared.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. forma2_imports holds a row or two, but the ACCESS EXCLUSIVE lock waits behind any open reader of it (a server's startup guard), and a migration should fail fast rather than queue every later query behind it.
SET LOCAL lock_timeout = '1s';

ALTER TABLE forma2_imports
    ADD COLUMN target_digests jsonb CHECK (jsonb_typeof(target_digests) = 'object'),
    ADD COLUMN source_marks jsonb CHECK (jsonb_typeof(source_marks) = 'object');

-- The row count of each forma2 table the import reads, and the newest of its updated_at (or "updatedAt") in milliseconds where it has one: what changes when forma2 runs. Tables that do not exist are left out, so it also runs on a database without forma2's tables. An update to a table without such a column that keeps its row count is not seen; the tables forma2 writes on every sign-up, response and edit have one.
-- +goose StatementBegin
CREATE FUNCTION forma2_marks() RETURNS jsonb AS $$
DECLARE
  forma2_table text;
  stamp text;
  mark jsonb;
  marks jsonb := '{}';
BEGIN
  FOREACH forma2_table IN ARRAY ARRAY[
    'Account', 'ActionClass', 'ApiKey', 'ApiKeyWorkspace', 'Contact', 'ContactAttribute', 'ContactAttributeKey', 'Display', 'EmbeddedData',
    'Integration', 'Invite', 'Language', 'Membership', 'Organization', 'OrganizationBilling', 'Response', 'ResponseQuotaLink', 'Segment',
    'Survey', 'SurveyAttributeFilter', 'SurveyEmbeddedData', 'SurveyFollowUp', 'SurveyLanguage', 'SurveyQuota', 'SurveyTrigger', 'Tag',
    'TagsOnResponses', 'Team', 'TeamUser', 'TwoFactor', 'User', 'Webhook', 'Workflow', 'WorkflowRun', 'WorkflowRunLog', 'WorkflowVersion',
    'Workspace', 'WorkspaceTeam'
  ] LOOP
    IF to_regclass(format('public.%I', forma2_table)) IS NULL THEN
      CONTINUE;
    END IF;
    SELECT c.column_name INTO stamp FROM information_schema.columns c
    WHERE c.table_schema = 'public' AND c.table_name = forma2_table AND c.column_name IN ('updated_at', 'updatedAt')
    ORDER BY c.column_name LIMIT 1;
    IF stamp IS NULL THEN
      EXECUTE format('SELECT jsonb_build_array(count(*), NULL) FROM public.%I', forma2_table) INTO mark;
    ELSE
      EXECUTE format('SELECT jsonb_build_array(count(*), (extract(epoch FROM max(%I)) * 1000)::bigint) FROM public.%I', stamp, forma2_table) INTO mark;
    END IF;
    marks := marks || jsonb_build_object(forma2_table, mark);
  END LOOP;
  RETURN marks;
END;
$$ LANGUAGE plpgsql STABLE;
-- +goose StatementEnd

-- The forma2 tables whose marks differ from the ones the latest record (an import or a discard) holds, in name order; empty when they are the same or the latest record holds none.
-- +goose StatementBegin
CREATE FUNCTION forma2_import_drift() RETURNS text[] AS $$
DECLARE
  recorded jsonb;
  current_marks jsonb;
BEGIN
  SELECT source_marks INTO recorded FROM forma2_imports ORDER BY completed_at DESC, id DESC LIMIT 1;
  IF recorded IS NULL THEN
    RETURN '{}';
  END IF;
  current_marks := forma2_marks();
  RETURN ARRAY(
    SELECT k FROM (SELECT jsonb_object_keys(recorded) AS k UNION SELECT jsonb_object_keys(current_marks)) keys
    WHERE recorded -> k IS DISTINCT FROM current_marks -> k
    ORDER BY k
  );
END;
$$ LANGUAGE plpgsql STABLE;
-- +goose StatementEnd

-- +goose Down

DROP FUNCTION forma2_import_drift();
DROP FUNCTION forma2_marks();
ALTER TABLE forma2_imports DROP COLUMN source_marks, DROP COLUMN target_digests;
