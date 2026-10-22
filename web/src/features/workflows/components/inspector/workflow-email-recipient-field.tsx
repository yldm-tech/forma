import { EyeOffIcon, MailIcon, TriangleAlertIcon, UserIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import {
  SelectContent,
  SelectItem,
  SelectMenu,
  SelectTrigger,
  SelectValue,
} from "../../../../components/ui/select-menu";
import { cn } from "../../../../lib/cn";
import {
  type EmailSendToOption,
  findEmailSendToOption,
} from "../../../survey-editor/follow-ups/lib/email-send-to-options";
import { getElementIconMap } from "../../../survey-editor/lib/elements";
import { WorkflowFieldError, WorkflowFieldLabel } from "./workflow-field";

const FIELD_ID = "workflow-email-to";
const ERROR_ID = "workflow-email-to-error";
const UNAVAILABLE_ID = "workflow-email-to-unavailable";

type TElementIconMap = ReturnType<typeof getElementIconMap>;

const getSelectItemIcon = (type: EmailSendToOption["type"], elementIconMap: TElementIconMap): ReactNode => {
  switch (type) {
    case "verifiedEmail":
      return <MailIcon className="size-4" />;
    case "hiddenField":
      return <EyeOffIcon className="size-4" />;
    case "user":
      return <UserIcon className="size-4" />;
    case "openTextElement":
      return <div className="size-4">{elementIconMap[TSurveyElementTypeEnum.OpenText]}</div>;
    case "contactInfoElement":
      return <div className="size-4">{elementIconMap[TSurveyElementTypeEnum.ContactInfo]}</div>;
  }
};

interface WorkflowEmailRecipientFieldProps {
  /** The recipients the bound survey offers; empty when there is nothing to pick. */
  options: EmailSendToOption[];
  /** The stored `to`: an element id, a hidden field id, an address or "verifiedEmail". */
  value: string;
  isEditable: boolean;
  isInvalid: boolean;
  onChange: (value: string) => void;
  /** Called when the list closes, this control's only "touched" moment. */
  onClose: () => void;
}

/** forma2's WorkflowEmailRecipientField: the send_email recipient picker over the bound survey's email elements, its hidden fields and the members who can access the workspace. */
export const WorkflowEmailRecipientField = ({
  options,
  value,
  isEditable,
  isInvalid,
  onChange,
  onClose,
}: Readonly<WorkflowEmailRecipientFieldProps>) => {
  const { t } = useTranslation();
  const elementIconMap = getElementIconMap(t);
  const optionsOfType = (...types: EmailSendToOption["type"][]) =>
    options.filter((option) => types.includes(option.type));

  // The verified email sits under "Questions" with the elements, as in the follow-ups' picker.
  const groups = [
    {
      label: t("common.questions"),
      options: optionsOfType("verifiedEmail", "openTextElement", "contactInfoElement"),
    },
    { label: t("common.hidden_fields"), options: optionsOfType("hiddenField") },
    { label: t("common.members"), options: optionsOfType("user") },
  ].filter((group) => group.options.length > 0);

  // A stored `to` no option matches (a member who lost access, an element a survey edit removed) is shown as its own flagged entry rather than as an empty field, while the definition still holds it and the runner still tries it.
  const matchedOption = findEmailSendToOption(options, value);
  const isValueUnavailable = value !== "" && !matchedOption;
  // The select matches by exact string, so it is driven by the matched option's id.
  const selectedValue = matchedOption?.id ?? (value || undefined);
  const describedBy = [isInvalid ? ERROR_ID : null, isValueUnavailable ? UNAVAILABLE_ID : null]
    .filter(Boolean)
    .join(" ");

  return (
    <div className="flex flex-col gap-2">
      <WorkflowFieldLabel htmlFor={FIELD_ID} isRequired isInvalid={isInvalid}>
        {t("workspace.workflows.email_to_label")}
      </WorkflowFieldLabel>
      <p className="text-xs text-slate-500">
        {t("workspace.surveys.edit.follow_ups_modal_action_to_description")}
      </p>
      {groups.length > 0 || isValueUnavailable ? (
        <SelectMenu
          value={selectedValue}
          disabled={!isEditable}
          onOpenChange={(isOpen) => {
            if (!isOpen) onClose();
          }}
          onValueChange={onChange}>
          <SelectTrigger
            id={FIELD_ID}
            aria-invalid={isInvalid}
            aria-describedby={describedBy || undefined}
            className={cn(
              "h-10 overflow-hidden bg-white text-ellipsis whitespace-nowrap",
              isInvalid && "border-red-500"
            )}>
            <SelectValue placeholder={t("workspace.workflows.email_to_placeholder")} />
          </SelectTrigger>
          <SelectContent>
            {groups.map((group) => (
              <div key={group.label} className="flex flex-col">
                <p className="p-2 text-sm text-slate-500">{group.label}</p>
                {group.options.map((option) => (
                  <SelectItem key={option.id} value={option.id}>
                    <div className="flex items-center gap-x-2">
                      {getSelectItemIcon(option.type, elementIconMap)}
                      <span className="overflow-hidden text-ellipsis whitespace-nowrap">{option.label}</span>
                    </div>
                  </SelectItem>
                ))}
              </div>
            ))}
            {isValueUnavailable ? (
              <div className="flex flex-col">
                <p className="p-2 text-sm text-slate-500">
                  {t("workspace.workflows.email_to_unavailable_group_label")}
                </p>
                <SelectItem value={value}>
                  <div className="text-warning-foreground flex items-center gap-x-2">
                    <TriangleAlertIcon className="text-warning size-4 shrink-0" aria-hidden="true" />
                    <span className="overflow-hidden text-ellipsis whitespace-nowrap">{value}</span>
                  </div>
                </SelectItem>
              </div>
            ) : null}
          </SelectContent>
        </SelectMenu>
      ) : (
        <div className="text-warning-foreground flex items-start gap-2">
          <TriangleAlertIcon className="text-warning mt-0.5 size-4 min-h-4 min-w-4" aria-hidden="true" />
          <p className="text-sm">{t("workspace.surveys.edit.follow_ups_modal_action_to_warning")}</p>
        </div>
      )}
      {isValueUnavailable ? (
        <p id={UNAVAILABLE_ID} className="text-warning-foreground text-sm">
          {t("workspace.workflows.email_to_unavailable_warning")}
        </p>
      ) : null}
      {isInvalid ? (
        <WorkflowFieldError id={ERROR_ID}>{t("workspace.workflows.email_to_required")}</WorkflowFieldError>
      ) : null}
    </div>
  );
};
