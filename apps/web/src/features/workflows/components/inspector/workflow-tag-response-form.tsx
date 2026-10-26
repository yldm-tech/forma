import { useQuery } from "@tanstack/react-query";
import { useAtomValue } from "jotai";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Select } from "../../../../components/ui/select";
import { tagsQuery } from "../../../../lib/queries/tags";
import type { TWorkflowTagResponseActionNode } from "../../definition";
import { useWorkflowNodeFieldFocus } from "../../hooks/use-workflow-node-field-focus";
import { workflowAtom } from "../../state/editor";
import { WorkflowFieldError, WorkflowFieldLabel } from "./workflow-field";

interface WorkflowTagResponseFormProps {
  node: TWorkflowTagResponseActionNode;
  isEditable: boolean;
  onChange: (next: TWorkflowTagResponseActionNode) => void;
}

/** The tag_response step: one of the workspace's tags, which the response gets when the step runs. A tag deleted since it was picked is flagged here and fails the step. */
export const WorkflowTagResponseForm = ({
  node,
  isEditable,
  onChange,
}: Readonly<WorkflowTagResponseFormProps>) => {
  const { t } = useTranslation();
  const workspaceId = useAtomValue(workflowAtom)?.workspaceId ?? "";
  const tags = useQuery({ ...tagsQuery(workspaceId), enabled: workspaceId !== "" });
  const [isTouched, setIsTouched] = useState(false);
  const { tagId } = node.config;

  useWorkflowNodeFieldFocus({
    nodeId: node.id,
    onRequest: () => setIsTouched(true),
    resolveElement: () => document.getElementById("workflow-tag-response-tag"),
  });

  const isGone = tagId !== "" && tags.isSuccess && !tags.data.some((tag) => tag.id === tagId);
  const error = isGone
    ? t("workspace.workflows.tag_response_tag_missing")
    : isTouched && tagId === ""
      ? t("workspace.workflows.tag_response_tag_required")
      : null;

  return (
    <div className="flex flex-col gap-2">
      <WorkflowFieldLabel htmlFor="workflow-tag-response-tag" isRequired isInvalid={Boolean(error)}>
        {t("workspace.workflows.tag_response_tag_label")}
      </WorkflowFieldLabel>
      <Select
        id="workflow-tag-response-tag"
        value={isGone ? "" : tagId}
        disabled={!isEditable || tags.isLoading}
        aria-invalid={Boolean(error)}
        aria-describedby={error ? "workflow-tag-response-tag-error" : "workflow-tag-response-tag-description"}
        onBlur={() => setIsTouched(true)}
        onChange={(event) => onChange({ ...node, config: { tagId: event.target.value } })}>
        <option value="" disabled>
          {tags.isLoading
            ? t("common.loading")
            : tags.data?.length
              ? t("workspace.workflows.tag_response_tag_placeholder")
              : t("workspace.workflows.tag_response_no_tags")}
        </option>
        {tags.data?.map((tag) => (
          <option key={tag.id} value={tag.id}>
            {tag.name}
          </option>
        ))}
      </Select>
      {error ? (
        <WorkflowFieldError id="workflow-tag-response-tag-error">{error}</WorkflowFieldError>
      ) : (
        <p id="workflow-tag-response-tag-description" className="text-xs text-slate-500">
          {t("workspace.workflows.tag_response_tag_description")}
        </p>
      )}
    </div>
  );
};
