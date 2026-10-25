-- Drops the bookkeeping of the retired forma2 data import: forma2_imports (00008, 00009), the record of an import or a discard that the startup guard and `forma import-forma2 --refresh` read, and the functions forma2_marks() and forma2_import_drift() (00009) that compared forma2's tables with that record. The import, its CLI and the guard are gone, so nothing reads them any more.
--
-- Only this server's own objects are dropped. A database can still hold forma2's tables ("User", "Survey", ...) next to this schema, possibly with rows that were never imported; they are left exactly as they are. No trigger calls either function, so none is dropped with them.
--
-- The Down section recreates the table and the functions as 00008 and 00009 left them, so the migrations before this one still go down, but the rows the table held are gone.

-- +goose Up

-- goose runs this migration in one transaction; SET LOCAL bounds only it. forma2_imports holds a row or two at most, but the ACCESS EXCLUSIVE lock DROP TABLE takes waits behind any open reader of it, and a migration should fail fast rather than queue every later query behind it.
SET LOCAL lock_timeout = '1s';

DROP FUNCTION forma2_import_drift();
DROP FUNCTION forma2_marks();
-- squawk-ignore ban-drop-table
DROP TABLE forma2_imports;

-- +goose Down

SET LOCAL lock_timeout = '1s';

CREATE TABLE forma2_imports (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    started_at timestamptz NOT NULL,
    completed_at timestamptz NOT NULL DEFAULT now(),
    outcome text NOT NULL CHECK (outcome IN ('imported', 'discarded')),
    legacy_namespace uuid,
    source_counts jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(source_counts) = 'object'),
    target_counts jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(target_counts) = 'object'),
    reason text,
    CHECK ((outcome = 'imported') = (legacy_namespace IS NOT NULL)),
    CHECK ((outcome = 'discarded') = (reason IS NOT NULL AND reason <> '')),
    target_digests jsonb CHECK (jsonb_typeof(target_digests) = 'object'),
    source_marks jsonb CHECK (jsonb_typeof(source_marks) = 'object')
);

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

