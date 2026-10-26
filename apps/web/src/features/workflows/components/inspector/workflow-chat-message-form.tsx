import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  type TWorkflowChatMessageActionNode,
  getWorkflowActionProblems,
  isValidChatWebhookUrl,
} from "../../definition";
import { useWorkflowNodeFieldFocus } from "../../hooks/use-workflow-node-field-focus";
import {
  WorkflowSecretField,
  WorkflowTemplateTextarea,
  WorkflowTestSendButton,
} from "./workflow-action-fields";

interface WorkflowChatMessageFormProps {
  node: TWorkflowChatMessageActionNode;
  isEditable: boolean;
  onChange: (next: TWorkflowChatMessageActionNode) => void;
}

const URL_PLACEHOLDERS: Record<TWorkflowChatMessageActionNode["actionType"], string> = {
  slack_message: "https://hooks.slack.com/services/…",
  discord_message: "https://discord.com/api/webhooks/…",
  teams_message: "https://….webhook.office.com/…",
};

/** The Slack, Discord and Microsoft Teams message steps: the channel's incoming-webhook URL, stored sealed since anyone holding it can post, a message template, and a test send. */
export const WorkflowChatMessageForm = ({
  node,
  isEditable,
  onChange,
}: Readonly<WorkflowChatMessageFormProps>) => {
  const { t } = useTranslation();
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
      document.getElementById(field === "message" ? "workflow-chat-message" : "workflow-chat-webhook-url"),
  });

  const enteredUrl = config.webhookUrl.value?.trim() ?? "";
  const urlError =
    enteredUrl !== "" && !isValidChatWebhookUrl(node.actionType, enteredUrl)
      ? t("workspace.workflows.chat_webhook_url_invalid")
      : isMissing("webhookUrl")
        ? t("workspace.workflows.chat_webhook_url_required")
        : null;
  const providers: Record<TWorkflowChatMessageActionNode["actionType"], string> = {
    slack_message: t("workspace.workflows.chat_webhook_url_description_slack"),
    discord_message: t("workspace.workflows.chat_webhook_url_description_discord"),
    teams_message: t("workspace.workflows.chat_webhook_url_description_teams"),
  };

  return (
    <div className="flex flex-col gap-4">
      <WorkflowSecretField
        id="workflow-chat-webhook-url"
        label={t("workspace.workflows.chat_webhook_url_label")}
        description={providers[node.actionType]}
        value={config.webhookUrl}
        isEditable={isEditable}
        isRequired
        placeholder={URL_PLACEHOLDERS[node.actionType]}
        error={urlError}
        onBlur={() => touch("webhookUrl")}
        onChange={(webhookUrl) => onChange({ ...node, config: { ...config, webhookUrl } })}
      />
      <WorkflowTemplateTextarea
        id="workflow-chat-message"
        label={t("workspace.workflows.chat_message_label")}
        value={config.message}
        isEditable={isEditable}
        isRequired
        rows={5}
        placeholder={t("workspace.workflows.chat_message_placeholder")}
        error={isMissing("message") ? t("workspace.workflows.chat_message_required") : null}
        onBlur={() => touch("message")}
        onChange={(message) => onChange({ ...node, config: { ...config, message } })}
      />
      <WorkflowTestSendButton stepId={node.id} isConfigured={problems.length === 0 && urlError === null} />
    </div>
  );
};
