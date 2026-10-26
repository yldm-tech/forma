import { useMutation } from "@tanstack/react-query";
import { useAtomValue } from "jotai";
import { CheckCircle2Icon, KeyRoundIcon, SendIcon, XCircleIcon } from "lucide-react";
import { type ReactNode, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { PasswordInput } from "../../../../components/ui/password-input";
import { cn } from "../../../../lib/cn";
import { getWorkflowApiErrorMessage, workflowsApi } from "../../api";
import type { TWorkflowSecretValue } from "../../definition";
import { resolveBoundTriggerSurvey } from "../../lib/authoring-context";
import { buildWorkflowTemplateVariables, insertAtSelection } from "../../lib/template-variables";
import {
  isWorkflowDirtyAtom,
  isWorkflowSavingAtom,
  workflowAtom,
  workflowDefinitionAtom,
} from "../../state/editor";
import { useWorkflowEmailAuthoringContext } from "../workflow-email-authoring-context";
import { WorkflowFieldError, WorkflowFieldLabel } from "./workflow-field";

// The controls the steps after send_email share: a secret that is entered once and then only shown by its hint, a template textarea with the variables it can use, and the test send.

const textareaClassName =
  "flex w-full rounded-md border border-slate-300 bg-transparent px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 focus:border-ink focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:outline-hidden disabled:cursor-not-allowed disabled:opacity-50";

interface WorkflowSecretFieldProps {
  id: string;
  label: ReactNode;
  description?: ReactNode;
  value: TWorkflowSecretValue;
  isEditable: boolean;
  isRequired?: boolean;
  /** The error to show under the field, already decided by the form. */
  error?: string | null;
  placeholder?: string;
  onChange: (next: TWorkflowSecretValue) => void;
  onBlur?: () => void;
}

/** A secret the server stores sealed: once saved it shows only its hint, and replacing it asks for the whole value again; the value typed is sent once and never shown back. */
export const WorkflowSecretField = ({
  id,
  label,
  description,
  value,
  isEditable,
  isRequired,
  error,
  placeholder,
  onChange,
  onBlur,
}: Readonly<WorkflowSecretFieldProps>) => {
  const { t } = useTranslation();
  const errorId = `${id}-error`;
  const isStored = Boolean(value.sealed);
  return (
    <div className="flex flex-col gap-2">
      <WorkflowFieldLabel htmlFor={id} isRequired={isRequired} isInvalid={Boolean(error)}>
        {label}
      </WorkflowFieldLabel>
      {description ? <p className="text-xs text-slate-500">{description}</p> : null}
      {isStored ? (
        <div className="flex items-center justify-between gap-2 rounded-md border border-slate-200 bg-slate-50 px-3 py-2">
          <span className="flex min-w-0 items-center gap-2 text-sm text-slate-700">
            <KeyRoundIcon className="size-4 shrink-0 text-slate-400" aria-hidden="true" />
            <span className="truncate">
              {value.hint
                ? t("workspace.workflows.secret_saved_hint", { hint: value.hint })
                : t("workspace.workflows.secret_saved")}
            </span>
          </span>
          {isEditable ? (
            <Button
              id={id}
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => onChange({ value: "" })}>
              {t("workspace.workflows.secret_replace")}
            </Button>
          ) : null}
        </div>
      ) : (
        <PasswordInput
          id={id}
          value={value.value ?? ""}
          disabled={!isEditable}
          autoComplete="off"
          placeholder={placeholder}
          aria-invalid={Boolean(error)}
          aria-describedby={error ? errorId : undefined}
          className={cn(error && "border-red-500")}
          onBlur={onBlur}
          onChange={(event) => onChange({ value: event.target.value })}
        />
      )}
      {error ? <WorkflowFieldError id={errorId}>{error}</WorkflowFieldError> : null}
    </div>
  );
};

interface WorkflowTemplateTextareaProps {
  id: string;
  label: ReactNode;
  description?: ReactNode;
  value: string;
  isEditable: boolean;
  isRequired?: boolean;
  error?: string | null;
  placeholder?: string;
  rows?: number;
  isMonospace?: boolean;
  onChange: (next: string) => void;
  onBlur?: () => void;
}

/** A template with the variables it can use beneath it; picking one puts its placeholder where the caret was. The variables come from the trigger's bound survey. */
export const WorkflowTemplateTextarea = ({
  id,
  label,
  description,
  value,
  isEditable,
  isRequired,
  error,
  placeholder,
  rows = 4,
  isMonospace,
  onChange,
  onBlur,
}: Readonly<WorkflowTemplateTextareaProps>) => {
  const { t } = useTranslation();
  const authoringContext = useWorkflowEmailAuthoringContext();
  const definition = useAtomValue(workflowDefinitionAtom);
  const survey = resolveBoundTriggerSurvey(authoringContext, definition);
  const variables = useMemo(() => buildWorkflowTemplateVariables(survey, t), [survey, t]);
  const ref = useRef<HTMLTextAreaElement>(null);
  const errorId = `${id}-error`;

  const insert = (token: string) => {
    const element = ref.current;
    const selection = element ? { start: element.selectionStart, end: element.selectionEnd } : null;
    const next = insertAtSelection(value, token, selection);
    onChange(next.value);
    requestAnimationFrame(() => {
      ref.current?.focus();
      ref.current?.setSelectionRange(next.caret, next.caret);
    });
  };

  return (
    <div className="flex flex-col gap-2">
      <WorkflowFieldLabel htmlFor={id} isRequired={isRequired} isInvalid={Boolean(error)}>
        {label}
      </WorkflowFieldLabel>
      {description ? <p className="text-xs text-slate-500">{description}</p> : null}
      <textarea
        ref={ref}
        id={id}
        rows={rows}
        value={value}
        disabled={!isEditable}
        placeholder={placeholder}
        spellCheck={!isMonospace}
        aria-invalid={Boolean(error)}
        aria-describedby={error ? errorId : undefined}
        className={cn(textareaClassName, isMonospace && "font-mono text-xs", error && "border-red-500")}
        onBlur={onBlur}
        onChange={(event) => onChange(event.target.value)}
      />
      {error ? <WorkflowFieldError id={errorId}>{error}</WorkflowFieldError> : null}
      {isEditable ? (
        <details className="rounded-md border border-slate-200 px-3 py-2 text-xs text-slate-600">
          <summary className="cursor-pointer select-none">
            {t("workspace.workflows.variables_insert")}
          </summary>
          <p className="mt-2 text-slate-500">{t("workspace.workflows.variables_description")}</p>
          <ul className="mt-2 flex max-h-40 flex-col gap-1 overflow-y-auto">
            {variables.map((variable) => (
              <li key={variable.token}>
                <button
                  type="button"
                  className="flex w-full items-center justify-between gap-2 rounded px-1 py-0.5 text-left hover:bg-slate-100"
                  onClick={() => insert(variable.token)}>
                  <span className="truncate text-slate-700">{variable.label}</span>
                  <code className="shrink-0 text-slate-500">{variable.token}</code>
                </button>
              </li>
            ))}
          </ul>
        </details>
      ) : null}
    </div>
  );
};

/** Sends the step once now, with the workflow's saved draft, and says how the receiver answered. Only while everything is saved, since the server sends what it stored. */
export const WorkflowTestSendButton = ({
  stepId,
  isConfigured,
}: Readonly<{ stepId: string; isConfigured: boolean }>) => {
  const { t } = useTranslation();
  const workflow = useAtomValue(workflowAtom);
  const isDirty = useAtomValue(isWorkflowDirtyAtom);
  const isSaving = useAtomValue(isWorkflowSavingAtom);
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(null);
  const mutation = useMutation({
    mutationFn: () => workflowsApi.testSend(workflow?.id ?? "", stepId),
    onSuccess: (res) =>
      setResult(
        res.ok
          ? { ok: true, message: t("workspace.workflows.test_send_succeeded", { status: res.status ?? 200 }) }
          : { ok: false, message: res.message ?? t("workspace.workflows.test_send_failed") }
      ),
    onError: (error) =>
      setResult({
        ok: false,
        message: getWorkflowApiErrorMessage(error, t("workspace.workflows.test_send_failed")),
      }),
  });
  const isUnsaved = isDirty || isSaving;
  return (
    <div className="flex flex-col gap-2 rounded-md border border-slate-200 p-3">
      <div className="flex items-center justify-between gap-3">
        <p className="text-xs text-slate-500">
          {isUnsaved
            ? t("workspace.workflows.test_send_save_first")
            : t("workspace.workflows.test_send_description")}
        </p>
        <Button
          type="button"
          variant="secondary"
          size="sm"
          loading={mutation.isPending}
          disabled={!workflow || !isConfigured || isUnsaved || mutation.isPending}
          onClick={() => {
            setResult(null);
            mutation.mutate();
          }}>
          <SendIcon />
          {t("workspace.workflows.test_send")}
        </Button>
      </div>
      {result ? (
        <p
          role="status"
          className={cn("flex items-start gap-1.5 text-xs", result.ok ? "text-green-700" : "text-red-600")}>
          {result.ok ? (
            <CheckCircle2Icon className="mt-px size-3.5 shrink-0" aria-hidden="true" />
          ) : (
            <XCircleIcon className="mt-px size-3.5 shrink-0" aria-hidden="true" />
          )}
          <span>{result.message}</span>
        </p>
      ) : null}
    </div>
  );
};
