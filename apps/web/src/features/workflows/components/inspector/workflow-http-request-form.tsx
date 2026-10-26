import { useAtomValue } from "jotai";
import { PlusIcon, Trash2Icon } from "lucide-react";
import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../../../components/ui/alert";
import { Button } from "../../../../components/ui/button";
import { Checkbox } from "../../../../components/ui/checkbox";
import { Input } from "../../../../components/ui/input";
import { Label } from "../../../../components/ui/label";
import { Select } from "../../../../components/ui/select";
import { Switch } from "../../../../components/ui/switch";
import {
  MAX_WORKFLOW_HTTP_HEADERS,
  type TWorkflowHttpHeader,
  type TWorkflowHttpRequestActionNode,
  WORKFLOW_HTTP_METHODS,
  getWorkflowActionProblems,
} from "../../definition";
import { useWorkflowNodeFieldFocus } from "../../hooks/use-workflow-node-field-focus";
import { clearSecretsOnOriginChange, generateSigningSecret } from "../../lib/action-config";
import { canManageWorkspaceAtom } from "../../state/editor";
import {
  WorkflowSecretField,
  WorkflowTemplateTextarea,
  WorkflowTestSendButton,
} from "./workflow-action-fields";
import { WorkflowFieldError, WorkflowFieldLabel } from "./workflow-field";

interface WorkflowHttpRequestFormProps {
  node: TWorkflowHttpRequestActionNode;
  isEditable: boolean;
  onChange: (next: TWorkflowHttpRequestActionNode) => void;
}

type TConfig = TWorkflowHttpRequestActionNode["config"];

/** The element a problem's field focuses: the URL, a header's name or secret, the body, the signing secret. */
const fieldElementId = (field: string): string => {
  const header = /^headers\.(\d+)\.(name|value|secret)$/.exec(field);
  if (header) return `workflow-http-header-${header[1]}-${header[2]}`;
  if (field === "body") return "workflow-http-body";
  if (field === "signingSecret") return "workflow-http-signing-secret";
  return "workflow-http-url";
};

/** The http_request step: a method, a URL, headers whose values may be secrets, a JSON body template, an optional Standard Webhooks signing secret, and a test send. Adding or changing one needs manage access to the workspace, so everyone else sees it read-only. */
export const WorkflowHttpRequestForm = ({
  node,
  isEditable: canEditDefinition,
  onChange,
}: Readonly<WorkflowHttpRequestFormProps>) => {
  const { t } = useTranslation();
  const canManage = useAtomValue(canManageWorkspaceAtom);
  const isEditable = canEditDefinition && canManage;
  const { config } = node;
  const [touched, setTouched] = useState<Set<string>>(new Set());
  const [secretsCleared, setSecretsCleared] = useState(false);
  const touch = useCallback(
    (...fields: string[]) =>
      setTouched((current) =>
        fields.every((field) => current.has(field)) ? current : new Set([...current, ...fields])
      ),
    []
  );
  const problems = getWorkflowActionProblems(node);
  const problemOf = (field: string) =>
    touched.has(field) ? problems.find((problem) => problem.field === field) : undefined;

  useWorkflowNodeFieldFocus({
    nodeId: node.id,
    onRequest: () => touch(...problems.map((problem) => problem.field)),
    resolveElement: (field) => document.getElementById(fieldElementId(field)),
  });

  const update = (next: Partial<TConfig>) => onChange({ ...node, config: { ...config, ...next } });
  const updateHeader = (index: number, header: TWorkflowHttpHeader) =>
    update({ headers: config.headers.map((existing, i) => (i === index ? header : existing)) });

  const handleUrlChange = (url: string) => {
    // Secrets are sealed for the URL's origin; a new host must not receive them, so they are asked for again.
    const next = clearSecretsOnOriginChange(config, url);
    if (next.cleared) setSecretsCleared(true);
    update(next.config);
  };

  const urlProblem = problemOf("url");
  const bodyProblem = problemOf("body");
  const signingProblem = problemOf("signingSecret");

  return (
    <div className="flex flex-col gap-4">
      {!canManage && canEditDefinition ? (
        <Alert variant="info" size="small">
          <AlertDescription>{t("workspace.workflows.http_manage_only")}</AlertDescription>
        </Alert>
      ) : null}

      <div className="flex flex-col gap-2">
        <WorkflowFieldLabel htmlFor="workflow-http-url" isRequired isInvalid={Boolean(urlProblem)}>
          {t("common.url")}
        </WorkflowFieldLabel>
        <div className="flex gap-2">
          <Select
            aria-label={t("workspace.workflows.http_method_label")}
            value={config.method}
            disabled={!isEditable}
            containerClassName="w-28 shrink-0"
            onChange={(event) => {
              const method = event.target.value as TConfig["method"];
              // A GET request carries no body; one left behind would keep the step from running.
              update(method === "GET" ? { method, body: "" } : { method });
            }}>
            {WORKFLOW_HTTP_METHODS.map((method) => (
              <option key={method} value={method}>
                {method}
              </option>
            ))}
          </Select>
          <Input
            id="workflow-http-url"
            type="url"
            value={config.url}
            disabled={!isEditable}
            placeholder="https://api.example.com/hooks/forma"
            isInvalid={Boolean(urlProblem)}
            aria-describedby={urlProblem ? "workflow-http-url-error" : "workflow-http-url-description"}
            onBlur={() => touch("url")}
            onChange={(event) => handleUrlChange(event.target.value)}
          />
        </div>
        {urlProblem ? (
          <WorkflowFieldError id="workflow-http-url-error">
            {urlProblem.invalid
              ? t("workspace.workflows.http_url_invalid")
              : t("workspace.workflows.http_url_required")}
          </WorkflowFieldError>
        ) : (
          <p id="workflow-http-url-description" className="text-xs text-slate-500">
            {t("workspace.workflows.http_url_description")}
          </p>
        )}
        {secretsCleared ? (
          <p className="text-xs text-amber-700">{t("workspace.workflows.http_secrets_cleared")}</p>
        ) : null}
      </div>

      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 text-sm font-medium text-slate-900">
          {t("workspace.workflows.http_headers_label")}
        </legend>
        {config.headers.length === 0 ? (
          <p className="text-xs text-slate-500">{t("workspace.workflows.http_headers_empty")}</p>
        ) : null}
        {config.headers.map((header, index) => {
          const nameProblem = problemOf(`headers.${index}.name`);
          const secretProblem = problemOf(`headers.${index}.secret`);
          const valueProblem = problemOf(`headers.${index}.value`);
          const isSecret = header.secret !== undefined;
          return (
            <div key={index} className="flex flex-col gap-2 rounded-md border border-slate-200 p-2">
              <div className="flex items-center gap-2">
                <Input
                  id={`workflow-http-header-${index}-name`}
                  aria-label={t("workspace.workflows.http_header_name_label")}
                  value={header.name}
                  disabled={!isEditable}
                  placeholder="Authorization"
                  isInvalid={Boolean(nameProblem)}
                  onBlur={() => touch(`headers.${index}.name`)}
                  onChange={(event) => updateHeader(index, { ...header, name: event.target.value })}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  disabled={!isEditable}
                  aria-label={t("workspace.workflows.http_header_remove")}
                  onClick={() => update({ headers: config.headers.filter((_, i) => i !== index) })}>
                  <Trash2Icon />
                </Button>
              </div>
              {nameProblem ? (
                <WorkflowFieldError id={`workflow-http-header-${index}-name-error`}>
                  {nameProblem.invalid
                    ? t("workspace.workflows.http_header_name_invalid")
                    : t("workspace.workflows.http_header_name_required")}
                </WorkflowFieldError>
              ) : null}
              {isSecret ? (
                <WorkflowSecretField
                  id={`workflow-http-header-${index}-secret`}
                  label={t("workspace.workflows.http_header_value_label")}
                  value={header.secret ?? {}}
                  isEditable={isEditable}
                  error={secretProblem ? t("workspace.workflows.secret_required") : null}
                  onBlur={() => touch(`headers.${index}.secret`)}
                  onChange={(secret) => updateHeader(index, { name: header.name, secret })}
                />
              ) : (
                <div className="flex flex-col gap-2">
                  <Input
                    id={`workflow-http-header-${index}-value`}
                    aria-label={t("workspace.workflows.http_header_value_label")}
                    value={header.value ?? ""}
                    disabled={!isEditable}
                    isInvalid={Boolean(valueProblem)}
                    onBlur={() => touch(`headers.${index}.value`)}
                    onChange={(event) => updateHeader(index, { ...header, value: event.target.value })}
                  />
                  {valueProblem ? (
                    <WorkflowFieldError id={`workflow-http-header-${index}-value-error`}>
                      {t("workspace.workflows.http_header_value_invalid")}
                    </WorkflowFieldError>
                  ) : null}
                </div>
              )}
              <label
                className="flex items-center gap-2 text-xs text-slate-600"
                htmlFor={`workflow-http-header-${index}-is-secret`}>
                <Checkbox
                  id={`workflow-http-header-${index}-is-secret`}
                  checked={isSecret}
                  disabled={!isEditable}
                  onCheckedChange={(checked) =>
                    updateHeader(
                      index,
                      checked === true
                        ? { name: header.name, secret: { value: header.value ?? "" } }
                        : { name: header.name, value: "" }
                    )
                  }
                />
                {t("workspace.workflows.http_header_secret_label")}
              </label>
            </div>
          );
        })}
        <Button
          type="button"
          variant="secondary"
          size="sm"
          className="w-fit"
          disabled={!isEditable || config.headers.length >= MAX_WORKFLOW_HTTP_HEADERS}
          onClick={() => update({ headers: [...config.headers, { name: "", value: "" }] })}>
          <PlusIcon />
          {t("workspace.workflows.http_header_add")}
        </Button>
      </fieldset>

      {config.method === "GET" ? null : (
        <WorkflowTemplateTextarea
          id="workflow-http-body"
          label={t("workspace.workflows.http_body_label")}
          description={t("workspace.workflows.http_body_description")}
          value={config.body}
          isEditable={isEditable}
          isMonospace
          rows={8}
          placeholder={'{\n  "response": "{{response.id}}",\n  "answers": "{{response.data}}"\n}'}
          error={bodyProblem ? t("workspace.workflows.http_body_invalid") : null}
          onBlur={() => touch("body")}
          onChange={(body) => update({ body })}
        />
      )}

      <div className="flex flex-col gap-3 rounded-md border border-slate-200 p-3">
        <div className="flex items-start justify-between gap-3">
          <div className="flex flex-col">
            <Label htmlFor="workflow-http-signing" className="text-sm font-medium">
              {t("workspace.workflows.http_signing_label")}
            </Label>
            <p className="text-xs text-slate-500">{t("workspace.workflows.http_signing_description")}</p>
          </div>
          <Switch
            id="workflow-http-signing"
            checked={config.signingSecret !== null}
            disabled={!isEditable}
            onCheckedChange={(checked) =>
              update({ signingSecret: checked ? { value: generateSigningSecret() } : null })
            }
          />
        </div>
        {config.signingSecret !== null ? (
          <>
            <WorkflowSecretField
              id="workflow-http-signing-secret"
              label={t("workspace.workflows.http_signing_secret_label")}
              description={
                config.signingSecret.sealed ? null : t("workspace.workflows.http_signing_secret_copy")
              }
              value={config.signingSecret}
              isEditable={isEditable}
              error={signingProblem ? t("workspace.workflows.secret_required") : null}
              onBlur={() => touch("signingSecret")}
              onChange={(signingSecret) => update({ signingSecret })}
            />
            {isEditable && !config.signingSecret.sealed ? (
              <Button
                type="button"
                variant="secondary"
                size="sm"
                className="w-fit"
                onClick={() => update({ signingSecret: { value: generateSigningSecret() } })}>
                {t("workspace.workflows.http_signing_secret_generate")}
              </Button>
            ) : null}
          </>
        ) : null}
      </div>

      {canManage ? <WorkflowTestSendButton stepId={node.id} isConfigured={problems.length === 0} /> : null}
    </div>
  );
};
