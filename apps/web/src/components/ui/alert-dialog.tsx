import { Button } from "./button";
import { Dialog, DialogBody, DialogFooter, DialogHeader, DialogTitle } from "./dialog";

interface AlertDialogProps {
  open: boolean;
  setOpen: (open: boolean) => void;
  headerText: string;
  mainText: string;
  confirmBtnLabel: string;
  declineBtnLabel?: string;
  declineBtnVariant?: "destructive" | "ghost" | "outline";
  onDecline?: () => void;
  onConfirm?: () => void;
  /** Adds a ghost button that backs out of the choice ("Keep editing"), for a dialog whose other two buttons both act. */
  cancelBtnLabel?: string;
  /** What the cancel button does; it closes the dialog by default. */
  onCancel?: () => void;
}

export const AlertDialog = ({
  open,
  setOpen,
  headerText,
  mainText,
  declineBtnLabel,
  onDecline,
  confirmBtnLabel,
  declineBtnVariant = "ghost",
  onConfirm,
  cancelBtnLabel,
  onCancel,
}: AlertDialogProps) => {
  return (
    <Dialog open={open} onOpenChange={setOpen} className="max-w-lg" hideCloseButton={true}>
      <DialogHeader>
        <DialogTitle>{headerText}</DialogTitle>
      </DialogHeader>
      <DialogBody>{mainText}</DialogBody>
      <DialogFooter>
        {cancelBtnLabel && (
          <Button variant="ghost" className="sm:mr-auto" onClick={onCancel ?? (() => setOpen(false))}>
            {cancelBtnLabel}
          </Button>
        )}
        {declineBtnLabel && onDecline && (
          <Button variant={declineBtnVariant} onClick={onDecline}>
            {declineBtnLabel}
          </Button>
        )}
        <Button
          onClick={() => {
            if (onConfirm) {
              onConfirm();
            } else {
              setOpen(false);
            }
          }}>
          {confirmBtnLabel}
        </Button>
      </DialogFooter>
    </Dialog>
  );
};
