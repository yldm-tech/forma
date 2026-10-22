import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../../components/ui/dialog";

/** forma2's DisableLinkModal: confirms switching a survey between its multi-use link and single-use links. */
export const DisableLinkModal = ({
  open,
  onOpenChange,
  type,
  onDisable,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  type: "multi-use" | "single-use";
  onDisable: () => void;
}) => {
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={onOpenChange} width="narrow" hideCloseButton disableCloseOnOutsideClick>
      <DialogHeader>
        <DialogTitle className="text-sm font-medium text-slate-900">
          {type === "multi-use"
            ? t("workspace.surveys.share.anonymous_links.disable_multi_use_link_modal_title")
            : t("common.are_you_sure")}
        </DialogTitle>
      </DialogHeader>
      <DialogBody>
        {type === "multi-use" ? (
          <>
            <p>{t("workspace.surveys.share.anonymous_links.disable_multi_use_link_modal_description")}</p>
            <br />
            <p>
              {t("workspace.surveys.share.anonymous_links.disable_multi_use_link_modal_description_subtext")}
            </p>
          </>
        ) : (
          <p>{t("workspace.surveys.share.anonymous_links.disable_single_use_link_modal_description")}</p>
        )}
      </DialogBody>
      <DialogFooter>
        <div className="flex w-full flex-col gap-2">
          <Button
            onClick={() => {
              onDisable();
              onOpenChange(false);
            }}>
            {type === "multi-use"
              ? t("workspace.surveys.share.anonymous_links.disable_multi_use_link_modal_button")
              : t("workspace.surveys.share.anonymous_links.disable_single_use_link_modal_button")}
          </Button>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
        </div>
      </DialogFooter>
    </Dialog>
  );
};
