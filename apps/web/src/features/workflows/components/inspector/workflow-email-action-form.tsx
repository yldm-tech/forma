import { useAtomValue, useSetAtom } from "jotai";
import { ArrowRightIcon } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../../../components/ui/alert";
import { Button } from "../../../../components/ui/button";
import { Editor } from "../../../../components/ui/editor";
import { Input } from "../../../../components/ui/input";
import { Label } from "../../../../components/ui/label";
import { Switch } from "../../../../components/ui/switch";
import FollowUpActionMultiEmailInput from "../../../survey-editor/follow-ups/components/follow-up-action-multi-email-input";
import {
  type EmailSendToOption,
  buildEmailSendToOptions,
} from "../../../survey-editor/follow-ups/lib/email-send-to-options";
import {
  type TWorkflowSendEmailActionNode,
  type TWorkflowSendEmailContentField,
  getBlankSendEmailContentFields,
} from "../../definition";
import { useWorkflowNodeFieldFocus } from "../../hooks/use-workflow-node-field-focus";
import { resolveBoundTriggerSurvey } from "../../lib/authoring-context";
import { insertAtSelection } from "../../lib/template-variables";
import { openWorkflowNodeConfigModalAtom, workflowDefinitionAtom } from "../../state/editor";
import { useWorkflowEmailAuthoringContext } from "../workflow-email-authoring-context";
import { WorkflowTemplateVariablePicker } from "./workflow-action-fields";
import { WorkflowEmailRecipientField } from "./workflow-email-recipient-field";
import { WorkflowFieldError, WorkflowFieldLabel } from "./workflow-field";

interface WorkflowEmailActionFormProps {
  node: TWorkflowSendEmailActionNode;
  isEditable: boolean;
  onChange: (next: TWorkflowSendEmailActionNode) => void;
}

// The slot recall and headline resolution use when no language is selected.
const DEFAULT_LANGUAGE_CODE = "default";

const HTML_OPENING_TAG_PATTERN = /<[a-z]/i;

// Whether some `<letter` is followed later by a `>`, found with index scans rather than a backtracking pattern.
const containsHtmlTag = (value: string): boolean => {
  const openingTag = HTML_OPENING_TAG_PATTERN.exec(value);
  if (!openingTag) return false;
  return value.lastIndexOf(">") > openingTag.index + 1;
};

const escapeHtml = (value: string): string =>
  value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");

/** Whether a body holds a `{{` placeholder, which the body never fills: it is written with recall (@), and only the subject takes `{{ }}`. */
export const hasTemplatePlaceholder = (body: string): boolean => body.includes("{{");

/** The body as the recall editor loads it, which takes HTML of block nodes only: a body that is already HTML passes through, a plain-text one (written through the API) is escaped and wrapped in paragraphs. */
export const toEditorHtml = (body: string): string => {
  if (!body) return "";
  if (containsHtmlTag(body)) return body;
  return body
    .split(/\n{2,}/)
    .map((paragraph) => `<p>${escapeHtml(paragraph).replaceAll("\n", "<br>")}</p>`)
    .join("");
};

// The controls a focus jump targets; the body is focused through its wrapper, since the editor owns a contenteditable.
const FIELD_INPUT_IDS: Record<Exclude<TWorkflowSendEmailContentField, "body">, string> = {
  to: "workflow-email-to",
  subject: "workflow-email-subject",
};

/** forma2's WorkflowEmailActionForm: the send_email step authored like a follow-up email, against the trigger's bound survey. */
export const WorkflowEmailActionForm = ({
  node,
  isEditable,
  onChange,
}: Readonly<WorkflowEmailActionFormProps>) => {
  const { t } = useTranslation();
  const authoringContext = useWorkflowEmailAuthoringContext();
  const definition = useAtomValue(workflowDefinitionAtom);
  const openNodeConfigModal = useSetAtom(openWorkflowNodeConfigModalAtom);
  const [firstRender, setFirstRender] = useState(true);

  // Which required fields may show their error: a new node stays clean until the user engaged with a field and left it empty, or arrived from the problems dialog.
  const [touchedFields, setTouchedFields] = useState<Partial<Record<TWorkflowSendEmailContentField, true>>>(
    {}
  );
  const markTouched = useCallback(
    (...fields: TWorkflowSendEmailContentField[]) =>
      setTouchedFields((current) =>
        fields.every((field) => current[field])
          ? current
          : { ...current, ...Object.fromEntries(fields.map((field) => [field, true as const])) }
      ),
    []
  );

  const bodyWrapperRef = useRef<HTMLDivElement>(null);
  const subjectRef = useRef<HTMLInputElement>(null);

  // Stable, since the editor re-registers its listener when this changes. Having had content is what touches the body: its serialized value keeps the paragraph even when blank.
  const handleBodyEmptyChange = useCallback(
    (isEmpty: boolean) => {
      if (!isEmpty) markTouched("body");
    },
    [markTouched]
  );

  const updateConfig = (next: Partial<TWorkflowSendEmailActionNode["config"]>) =>
    onChange({ ...node, config: { ...node.config, ...next } });

  // A picked variable goes where the caret was in the subject, which keeps the focus.
  const insertIntoSubject = (token: string) => {
    const element = subjectRef.current;
    const selection = element
      ? {
          start: element.selectionStart ?? node.config.subject.length,
          end: element.selectionEnd ?? node.config.subject.length,
        }
      : null;
    const next = insertAtSelection(node.config.subject, token, selection);
    updateConfig({ subject: next.value });
    requestAnimationFrame(() => {
      subjectRef.current?.focus();
      subjectRef.current?.setSelectionRange(next.caret, next.caret);
    });
  };

  const invalidFields = new Set(
    getBlankSendEmailContentFields(node.config).filter((field) => touchedFields[field])
  );

  // Arriving from the problems dialog reveals every missing field on this node and focuses the one it pointed at.
  useWorkflowNodeFieldFocus({
    nodeId: node.id,
    onRequest: () => markTouched(...getBlankSendEmailContentFields(node.config)),
    resolveElement: (field) =>
      field === "body"
        ? bodyWrapperRef.current?.querySelector<HTMLElement>('[contenteditable="true"]')
        : document.getElementById(FIELD_INPUT_IDS[field as keyof typeof FIELD_INPUT_IDS]),
  });

  const triggerSurveyId = definition?.trigger?.type === "trigger" ? definition.trigger.config.surveyId : null;
  const survey = resolveBoundTriggerSurvey(authoringContext, definition);

  // A new trigger survey leaves `to` (an element id) and the body's recall tokens dangling against the previous survey's elements, so both are cleared, as the trigger form clears its endings. The first mount never wipes saved values.
  const previousTriggerSurveyId = useRef<string | null>(triggerSurveyId);
  const latest = useRef({ node, onChange });
  useEffect(() => {
    latest.current = { node, onChange };
  });
  useEffect(() => {
    if (previousTriggerSurveyId.current === triggerSurveyId) return;
    previousTriggerSurveyId.current = triggerSurveyId;
    const current = latest.current.node;
    if (current.config.to === "" && current.config.body === "") return;
    latest.current.onChange({ ...current, config: { ...current.config, to: "", body: "" } });
    // They were wiped from under the user, so their errors are exactly what they need to see.
    markTouched("to", "body");
  }, [triggerSurveyId, markTouched]);

  const emailSendToOptions: EmailSendToOption[] = useMemo(() => {
    if (!survey || !authoringContext) return [];
    return buildEmailSendToOptions({
      survey,
      teamMemberDetails: authoringContext.teamMemberDetails,
      userEmail: authoringContext.userEmail,
      selectedLanguageCode: DEFAULT_LANGUAGE_CODE,
      t,
    });
  }, [survey, authoringContext, t]);

  // Without a bound survey there is nothing to author against: the recipients and the recall body both come from it. Point at the trigger instead.
  if (!survey) {
    return (
      <div className="flex flex-col gap-3 rounded-md border border-slate-200 bg-slate-50 p-3">
        <p className="text-sm text-slate-600">{t("workspace.workflows.email_needs_survey")}</p>
        {definition?.trigger ? (
          <Button
            type="button"
            variant="secondary"
            size="sm"
            className="w-fit"
            onClick={() => {
              const triggerId = definition.trigger?.id;
              if (triggerId) openNodeConfigModal(triggerId);
            }}>
            {t("workspace.workflows.email_set_up_trigger")}
            <ArrowRightIcon />
          </Button>
        ) : null}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <WorkflowEmailRecipientField
        options={emailSendToOptions}
        value={node.config.to}
        isEditable={isEditable}
        isInvalid={invalidFields.has("to")}
        onChange={(value) => updateConfig({ to: value })}
        onClose={() => markTouched("to")}
      />

      <div className="flex flex-col gap-2">
        <Label>{t("workspace.workflows.email_from_label")}</Label>
        <p className="text-xs text-slate-500">
          {t("workspace.surveys.edit.follow_ups_modal_action_from_description")}
        </p>
        <div className="w-fit rounded-md border border-slate-200 bg-slate-100 px-2 py-1">
          {/* The deployment's MAIL_FROM, which mail always leaves from; config.from is vestigial. */}
          <span className="text-sm text-slate-900">{authoringContext?.mailFrom ?? node.config.from}</span>
        </div>
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="workflow-email-reply-to">{t("workspace.workflows.email_reply_to_label")}</Label>
        <p className="text-xs text-slate-500">
          {t("workspace.surveys.edit.follow_ups_modal_action_replyTo_description")}
        </p>
        <FollowUpActionMultiEmailInput
          disabled={!isEditable}
          emails={node.config.replyTo}
          setEmails={(update) => {
            const nextReplyTo = typeof update === "function" ? update(node.config.replyTo) : update;
            updateConfig({ replyTo: nextReplyTo });
          }}
        />
      </div>

      <div className="flex flex-col gap-2">
        <WorkflowFieldLabel
          htmlFor="workflow-email-subject"
          isRequired
          isInvalid={invalidFields.has("subject")}>
          {t("workspace.workflows.email_subject_label")}
        </WorkflowFieldLabel>
        <Input
          ref={subjectRef}
          id="workflow-email-subject"
          value={node.config.subject}
          disabled={!isEditable}
          placeholder={t("workspace.workflows.email_subject_placeholder")}
          isInvalid={invalidFields.has("subject")}
          aria-invalid={invalidFields.has("subject")}
          aria-describedby={invalidFields.has("subject") ? "workflow-email-subject-error" : undefined}
          onBlur={() => markTouched("subject")}
          onChange={(event) => updateConfig({ subject: event.target.value })}
        />
        {invalidFields.has("subject") ? (
          <WorkflowFieldError id="workflow-email-subject-error">
            {t("workspace.workflows.email_subject_required")}
          </WorkflowFieldError>
        ) : null}
        {isEditable ? <WorkflowTemplateVariablePicker onInsert={insertIntoSubject} /> : null}
      </div>

      {/* Four lines of body by default rather than the editor's two. */}
      <div className="flex flex-col gap-2 [--editor-min-height:96px]" ref={bodyWrapperRef}>
        <WorkflowFieldLabel id="workflow-email-body-label" isRequired isInvalid={invalidFields.has("body")}>
          {t("workspace.workflows.email_body_label")}
        </WorkflowFieldLabel>
        <Editor
          id="workflow-email-body-label"
          disableLists
          excludedToolbarItems={["blockType"]}
          getText={() => toEditorHtml(node.config.body)}
          setText={(value: string) => updateConfig({ body: value })}
          onEmptyChange={handleBodyEmptyChange}
          isInvalid={invalidFields.has("body")}
          ariaDescribedBy={invalidFields.has("body") ? "workflow-email-body-error" : undefined}
          firstRender={firstRender}
          setFirstRender={setFirstRender}
          editable={isEditable}
          placeholder={t("workspace.workflows.email_body_placeholder")}
          localSurvey={survey}
          elementId={node.id}
          selectedLanguageCode={DEFAULT_LANGUAGE_CODE}
        />
        {invalidFields.has("body") ? (
          <WorkflowFieldError id="workflow-email-body-error">
            {t("workspace.workflows.email_body_required")}
          </WorkflowFieldError>
        ) : null}
        {hasTemplatePlaceholder(node.config.body) ? (
          <Alert variant="warning" size="small">
            <AlertDescription className="whitespace-normal">
              {t("workspace.workflows.email_body_placeholder_warning", { example: "{{answers.nps}}" })}
            </AlertDescription>
          </Alert>
        ) : null}
      </div>

      <div className="flex items-start justify-between gap-3 rounded-md border border-slate-200 p-3">
        <div className="flex flex-col">
          <Label htmlFor="workflow-email-attach-response-data" className="text-sm font-medium">
            {t("workspace.workflows.email_attach_response_data_label")}
          </Label>
          <p className="text-xs text-slate-500">
            {t("workspace.workflows.email_attach_response_data_description")}
          </p>
        </div>
        <Switch
          id="workflow-email-attach-response-data"
          checked={node.config.attachResponseData}
          disabled={!isEditable}
          onCheckedChange={(checked) => updateConfig({ attachResponseData: checked })}
        />
      </div>

      {node.config.attachResponseData ? (
        <div className="flex flex-col gap-3 rounded-md border border-slate-200 p-3">
          <div className="flex items-start justify-between gap-3">
            <Label htmlFor="workflow-email-include-variables" className="text-sm font-medium">
              {t("workspace.workflows.email_include_variables_label")}
            </Label>
            <Switch
              id="workflow-email-include-variables"
              checked={node.config.includeVariables ?? false}
              disabled={!isEditable}
              onCheckedChange={(checked) => updateConfig({ includeVariables: checked })}
            />
          </div>
          <div className="flex items-start justify-between gap-3">
            <Label htmlFor="workflow-email-include-hidden-fields" className="text-sm font-medium">
              {t("workspace.workflows.email_include_hidden_fields_label")}
            </Label>
            <Switch
              id="workflow-email-include-hidden-fields"
              checked={node.config.includeHiddenFields ?? false}
              disabled={!isEditable}
              onCheckedChange={(checked) => updateConfig({ includeHiddenFields: checked })}
            />
          </div>
        </div>
      ) : null}
    </div>
  );
};
