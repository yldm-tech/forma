import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TActionClass as ActionClass } from "@forma/types/action-classes";
import { TSurvey } from "@forma/types/surveys/types";
import { CreateNewActionTab } from "./create-new-action-tab";
import { SavedActionsTab } from "./saved-actions-tab";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "../../../components/ui/dialog";

interface AddActionModalProps {
  open: boolean;
  setOpen: React.Dispatch<React.SetStateAction<boolean>>;
  workspaceId: string;
  actionClasses: ActionClass[];
  isReadOnly: boolean;
  localSurvey: TSurvey;
  setLocalSurvey: React.Dispatch<React.SetStateAction<TSurvey>>;
}

export const AddActionModal = ({
  open,
  setOpen,
  actionClasses,
  localSurvey,
  setLocalSurvey,
  isReadOnly,
  workspaceId,
}: AddActionModalProps) => {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState(0);

  const tabs = [
    {
      title: t("workspace.surveys.edit.select_saved_action"),
      children: (
        <SavedActionsTab
          actionClasses={actionClasses}
          localSurvey={localSurvey}
          setLocalSurvey={setLocalSurvey}
          setOpen={setOpen}
        />
      ),
    },
    {
      title: t("workspace.surveys.edit.capture_new_action"),
      children: (
        <CreateNewActionTab
          actionClasses={actionClasses}
          setOpen={setOpen}
          isReadOnly={isReadOnly}
          setLocalSurvey={setLocalSurvey}
          workspaceId={workspaceId}
        />
      ),
    },
  ];

  const handleTabClick = (index: number) => {
    setActiveTab(index);
  };

  useEffect(() => {
    if (!open) {
      setActiveTab(0);
    }
  }, [open]);

  return (
    <Dialog open={open} onOpenChange={setOpen} disableCloseOnOutsideClick>
      <DialogHeader>
        <DialogTitle>{t("common.add_action")}</DialogTitle>
        <DialogDescription>
          {t("workspace.surveys.edit.capture_a_new_action_to_trigger_a_survey_on")}
        </DialogDescription>
      </DialogHeader>
      <DialogBody>
        <div className="flex h-full w-full items-center justify-center gap-x-2 border-b border-slate-200 px-6">
          {tabs.map((tab, index) => (
            <button
              type="button"
              key={tab.title}
              className={`mr-4 px-1 pb-3 focus:outline-hidden ${
                activeTab === index
                  ? "border-brand-dark border-b-2 font-semibold text-slate-900"
                  : "text-slate-500 hover:text-slate-700"
              }`}
              onClick={() => handleTabClick(index)}>
              {tab.title}
            </button>
          ))}
        </div>
        <div className="flex-1 pt-4">{tabs[activeTab]?.children}</div>
      </DialogBody>
    </Dialog>
  );
};
