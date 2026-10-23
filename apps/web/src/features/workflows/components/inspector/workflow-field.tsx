import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Label } from "../../../../components/ui/label";
import { cn } from "../../../../lib/cn";

// The label and inline error of the inspector's config forms. The forms have no submit (validity comes from the definition, which autosaves), so these carry the visual contract of the form components without a form library.

interface WorkflowFieldLabelProps {
  htmlFor?: string;
  /** For a control htmlFor cannot reach: the rich-text editor's contenteditable names itself through this id. */
  id?: string;
  /** Shows the required marker; the accessible name carries the word, not the glyph. */
  isRequired?: boolean;
  isInvalid?: boolean;
  children: ReactNode;
}

export const WorkflowFieldLabel = ({
  htmlFor,
  id,
  isRequired,
  isInvalid,
  children,
}: Readonly<WorkflowFieldLabelProps>) => {
  const { t } = useTranslation();
  return (
    <Label htmlFor={htmlFor} id={id} className={cn("block", isInvalid && "text-red-500")}>
      {children}
      {isRequired ? (
        <>
          <span aria-hidden="true" className="ml-0.5 text-red-500">
            *
          </span>
          <span className="sr-only"> ({t("common.required")})</span>
        </>
      ) : null}
    </Label>
  );
};

/** id is what the control's aria-describedby names, so the message is announced with it. */
export const WorkflowFieldError = ({ id, children }: Readonly<{ id: string; children: ReactNode }>) => (
  <p id={id} className="text-error text-sm">
    {children}
  </p>
);
