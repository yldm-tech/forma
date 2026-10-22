import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "./button";
import { Dialog, DialogBody, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "./dialog";

/** forma2's ConfirmationModal: a question, its consequence, Cancel and a confirm button (destructive unless told otherwise). */
export const ConfirmationModal = ({
  title,
  open,
  setOpen,
  onConfirm,
  description,
  body,
  buttonText,
  isButtonDisabled = false,
  buttonVariant = "destructive",
  buttonLoading = false,
  closeOnOutsideClick = true,
  hideCloseButton = false,
  cancelButtonText,
}: {
  title: string;
  open: boolean;
  setOpen: (open: boolean) => void;
  onConfirm: () => void;
  description?: string;
  body: ReactNode;
  buttonText: string;
  isButtonDisabled?: boolean;
  buttonVariant?: "destructive" | "default";
  buttonLoading?: boolean;
  closeOnOutsideClick?: boolean;
  hideCloseButton?: boolean;
  cancelButtonText?: string;
}) => {
  const { t } = useTranslation();
  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      width="narrow"
      hideCloseButton={hideCloseButton}
      disableCloseOnOutsideClick={!closeOnOutsideClick}>
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        {description && <DialogDescription>{description}</DialogDescription>}
      </DialogHeader>
      <DialogBody>
        <div className="text-sm whitespace-pre-wrap text-slate-600">{body}</div>
      </DialogBody>
      <DialogFooter>
        <Button variant="secondary" onClick={() => setOpen(false)} disabled={buttonLoading}>
          {cancelButtonText ?? t("common.cancel")}
        </Button>
        <Button
          variant={buttonVariant}
          loading={buttonLoading}
          disabled={isButtonDisabled}
          onClick={() => {
            if (!isButtonDisabled) onConfirm();
          }}>
          {buttonText}
        </Button>
      </DialogFooter>
    </Dialog>
  );
};
