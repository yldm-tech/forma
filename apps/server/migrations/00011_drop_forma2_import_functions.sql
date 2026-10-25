-- Drops the functions of the retired forma2 data import: forma2_marks() and forma2_import_drift() (00009), which compared forma2's tables with the latest import or discard recorded in forma2_imports. The import, its CLI and the startup guard that called them are gone, so nothing calls them any more. No trigger calls either function.
--
-- forma2_imports itself stays. A release before this one refuses to start on a database whose forma2 "User" table has rows and that has no forma2_imports table, so dropping it would stop an image rollback on any database that imported forma2. That release's guard skips the drift check when forma2_import_drift() is missing, so dropping the functions does not stop it. Only this server's own objects are touched: forma2's tables ("User", "Survey", ...) that a database may still hold next to this schema are left exactly as they are.
--
-- The Down section recreates the two functions as 00009 left them.

-- +goose Up

DROP FUNCTION forma2_import_drift();
DROP FUNCTION forma2_marks();

-- +goose Down

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

