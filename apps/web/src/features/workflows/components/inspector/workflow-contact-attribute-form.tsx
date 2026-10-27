import { useQuery } from "@tanstack/react-query";
import { useAtomValue } from "jotai";
import { ShieldAlertIcon } from "lucide-react";
import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../../../components/ui/alert";
import { ConfirmationModal } from "../../../../components/ui/confirmation-modal";
import { Select } from "../../../../components/ui/select";
import { attributeKeysQuery } from "../../../../lib/queries/contacts";
import { sdkIdentityQuery } from "../../../../lib/queries/workspaces";
import { type TWorkflowSetContactAttributeActionNode, getWorkflowActionProblems } from "../../definition";
import { useWorkflowNodeFieldFocus } from "../../hooks/use-workflow-node-field-focus";
import { isRespondentSourcedValue, needsProtectedWriteConfirmation } from "../../lib/protected-attributes";
import { workflowAtom } from "../../state/editor";
import { WorkflowTemplateTextarea } from "./workflow-action-fields";
import { WorkflowFieldError, WorkflowFieldLabel } from "./workflow-field";

interface WorkflowContactAttributeFormProps {
  node: TWorkflowSetContactAttributeActionNode;
  isEditable: boolean;
  onChange: (next: TWorkflowSetContactAttributeActionNode) => void;
}

/** The set_contact_attribute step: an existing attribute of the workspace's contacts and a template for its value, written onto the response's contact. Unique attributes (email, userId) identify a contact and are not offered. Attributes protected from the SDK are marked, and making the step write one from what the respondent entered asks for a confirmation first; the step may still do it. */
export const WorkflowContactAttributeForm = ({
  node,
  isEditable,
  onChange,
}: Readonly<WorkflowContactAttributeFormProps>) => {
  const { t } = useTranslation();
  const workspaceId = useAtomValue(workflowAtom)?.workspaceId ?? "";
  const keys = useQuery({ ...attributeKeysQuery(workspaceId), enabled: workspaceId !== "" });
  const identity = useQuery({ ...sdkIdentityQuery(workspaceId), enabled: workspaceId !== "" });
  const protectedKeys = identity.data?.protectedAttributeKeys ?? [];
  const { config } = node;
  // A change that makes the step write a protected attribute from the response, or moves such a write to another protected attribute, waits here for the confirmation; one that keeps an already confirmed (or saved) write on the same attribute goes straight through.
  const [pendingConfig, setPendingConfig] = useState<TWorkflowSetContactAttributeActionNode["config"] | null>(
    null
  );
  const changeConfig = (next: TWorkflowSetContactAttributeActionNode["config"]) => {
    if (needsProtectedWriteConfirmation(config, next, protectedKeys)) {
      setPendingConfig(next);
      return;
    }
    onChange({ ...node, config: next });
  };
  const [touched, setTouched] = useState<Set<string>>(new Set());
  const touch = useCallback(
    (...fields: string[]) =>
      setTouched((current) =>
        fields.every((field) => current.has(field)) ? current : new Set([...current, ...fields])
      ),
    []
  );
  const problems = getWorkflowActionProblems(node);
  const isMissing = (field: string) =>
    touched.has(field) && problems.some((problem) => problem.field === field);

  useWorkflowNodeFieldFocus({
    nodeId: node.id,
    onRequest: () => touch(...problems.map((problem) => problem.field)),
    resolveElement: (field) =>
      document.getElementById(
        field === "value" ? "workflow-contact-attribute-value" : "workflow-contact-attribute-key"
      ),
  });

  const writableKeys = keys.data?.filter((key) => !key.isUnique) ?? [];
  const selectedKey = writableKeys.find((key) => key.key === config.attributeKey);
  const isSelectedProtected = selectedKey !== undefined && protectedKeys.includes(selectedKey.key);
  const isGone = config.attributeKey !== "" && keys.isSuccess && !selectedKey;
  const keyError = isGone
    ? t("workspace.workflows.contact_attribute_key_missing")
    : isMissing("attributeKey")
      ? t("workspace.workflows.contact_attribute_key_required")
      : null;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <WorkflowFieldLabel htmlFor="workflow-contact-attribute-key" isRequired isInvalid={Boolean(keyError)}>
          {t("workspace.workflows.contact_attribute_key_label")}
        </WorkflowFieldLabel>
        <Select
          id="workflow-contact-attribute-key"
          value={isGone ? "" : config.attributeKey}
          disabled={!isEditable || keys.isLoading}
          aria-invalid={Boolean(keyError)}
          aria-describedby={keyError ? "workflow-contact-attribute-key-error" : undefined}
          onBlur={() => touch("attributeKey")}
          onChange={(event) => changeConfig({ ...config, attributeKey: event.target.value })}>
          <option value="" disabled>
            {keys.isLoading
              ? t("common.loading")
              : t("workspace.workflows.contact_attribute_key_placeholder")}
          </option>
          {writableKeys.map((key) => {
            const label = key.name ? `${key.name} (${key.key})` : key.key;
            return (
              <option key={key.id} value={key.key}>
                {protectedKeys.includes(key.key)
                  ? t("workspace.workflows.contact_attribute_protected_option", { label })
                  : label}
              </option>
            );
          })}
        </Select>
        {keyError ? (
          <WorkflowFieldError id="workflow-contact-attribute-key-error">{keyError}</WorkflowFieldError>
        ) : null}
        {isSelectedProtected ? (
          <Alert
            variant={isRespondentSourcedValue(config.value, protectedKeys) ? "warning" : "info"}
            size="small">
            <AlertDescription className="whitespace-normal">
              {isRespondentSourcedValue(config.value, protectedKeys)
                ? t("workspace.workflows.contact_attribute_protected_from_response", {
                    key: config.attributeKey,
                  })
                : t("workspace.workflows.contact_attribute_protected_notice", { key: config.attributeKey })}
            </AlertDescription>
          </Alert>
        ) : null}
      </div>
      <WorkflowTemplateTextarea
        id="workflow-contact-attribute-value"
        label={t("workspace.workflows.contact_attribute_value_label")}
        description={
          selectedKey?.dataType === "number"
            ? t("workspace.workflows.contact_attribute_value_number")
            : selectedKey?.dataType === "date"
              ? t("workspace.workflows.contact_attribute_value_date")
              : t("workspace.workflows.contact_attribute_value_description")
        }
        value={config.value}
        isEditable={isEditable}
        isRequired
        rows={2}
        error={isMissing("value") ? t("workspace.workflows.contact_attribute_value_required") : null}
        onBlur={() => touch("value")}
        onChange={(value) => changeConfig({ ...config, value })}
      />
      <ConfirmationModal
        open={pendingConfig !== null}
        setOpen={(open) => {
          if (!open) setPendingConfig(null);
        }}
        title={t("workspace.workflows.contact_attribute_protected_confirm_title")}
        description={t("workspace.workflows.protected_attribute_confirm_description")}
        body={t("workspace.workflows.contact_attribute_protected_confirm_body", {
          key: pendingConfig?.attributeKey ?? "",
        })}
        buttonText={t("workspace.workflows.contact_attribute_protected_confirm_button")}
        Icon={ShieldAlertIcon}
        onConfirm={() => {
          if (pendingConfig) onChange({ ...node, config: pendingConfig });
          setPendingConfig(null);
        }}
      />
    </div>
  );
};
