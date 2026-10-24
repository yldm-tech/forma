import { CircleAlert, TrashIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "./button";
import { Dialog, DialogBody, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "./dialog";

interface DeleteDialogProps {
  open: boolean;
  setOpen: (open: boolean) => void;
  deleteWhat: string;
  onDelete: () => void;
  text?: string;
  isDeleting?: boolean;
  disabled?: boolean;
  children?: ReactNode;
  buttonLabel?: string;
}

/** forma2's DeleteDialog: a confirmation that a stray click cannot dismiss. Escape cancels it like the Cancel button, except while the delete is in flight. */
export const DeleteDialog = ({
  open,
  setOpen,
  deleteWhat,
  onDelete,
  text,
  isDeleting,
  disabled,
  children,
  buttonLabel,
}: DeleteDialogProps) => {
  const { t } = useTranslation();
  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      width="narrow"
      role="alertdialog"
      hideCloseButton
      disableCloseOnOutsideClick
      allowEscapeClose={!isDeleting}>
      <DialogHeader>
        <div className="flex items-center gap-2">
          <CircleAlert className="size-4" />
          <div>
            <DialogTitle>{t("common.delete_what", { deleteWhat })}</DialogTitle>
            <DialogDescription>{t("workspace.general.this_action_cannot_be_undone")}</DialogDescription>
          </div>
        </div>
      </DialogHeader>
      <DialogBody>
        {text && <p>{text}</p>}
        {children && <div>{children}</div>}
      </DialogBody>
      <DialogFooter>
        <Button variant="secondary" onClick={() => setOpen(false)} disabled={isDeleting}>
          {t("common.cancel")}
        </Button>
        <Button
          variant="destructive"
          onClick={onDelete}
          loading={isDeleting}
          disabled={disabled || isDeleting}>
          <TrashIcon />
          {buttonLabel ?? t("common.delete")}
        </Button>
      </DialogFooter>
    </Dialog>
  );
};
