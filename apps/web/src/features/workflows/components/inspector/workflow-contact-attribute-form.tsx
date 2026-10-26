import { useQuery } from "@tanstack/react-query";
import { useAtomValue } from "jotai";
import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { Select } from "../../../../components/ui/select";
import { attributeKeysQuery } from "../../../../lib/queries/contacts";
import { type TWorkflowSetContactAttributeActionNode, getWorkflowActionProblems } from "../../definition";
import { useWorkflowNodeFieldFocus } from "../../hooks/use-workflow-node-field-focus";
import { workflowAtom } from "../../state/editor";
import { WorkflowTemplateTextarea } from "./workflow-action-fields";
import { WorkflowFieldError, WorkflowFieldLabel } from "./workflow-field";

interface WorkflowContactAttributeFormProps {
  node: TWorkflowSetContactAttributeActionNode;
  isEditable: boolean;
  onChange: (next: TWorkflowSetContactAttributeActionNode) => void;
}

/** The set_contact_attribute step: an existing attribute of the workspace's contacts and a template for its value, written onto the response's contact. Unique attributes (email, userId) identify a contact and are not offered. */
export const WorkflowContactAttributeForm = ({
  node,
  isEditable,
  onChange,
}: Readonly<WorkflowContactAttributeFormProps>) => {
  const { t } = useTranslation();
  const workspaceId = useAtomValue(workflowAtom)?.workspaceId ?? "";
  const keys = useQuery({ ...attributeKeysQuery(workspaceId), enabled: workspaceId !== "" });
  const { config } = node;
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
          onChange={(event) =>
            onChange({ ...node, config: { ...config, attributeKey: event.target.value } })
          }>
          <option value="" disabled>
            {keys.isLoading
              ? t("common.loading")
              : t("workspace.workflows.contact_attribute_key_placeholder")}
          </option>
          {writableKeys.map((key) => (
            <option key={key.id} value={key.key}>
              {key.name ? `${key.name} (${key.key})` : key.key}
            </option>
          ))}
        </Select>
        {keyError ? (
          <WorkflowFieldError id="workflow-contact-attribute-key-error">{keyError}</WorkflowFieldError>
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
        onChange={(value) => onChange({ ...node, config: { ...config, value } })}
      />
    </div>
  );
};
