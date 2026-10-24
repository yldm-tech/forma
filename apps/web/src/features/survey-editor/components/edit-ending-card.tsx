import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { createId } from "@paralleldrive/cuid2";
import * as Collapsible from "@radix-ui/react-collapsible";
import { GripIcon, Handshake, Undo2 } from "lucide-react";
import { useState } from "react";
import { toast } from "../../../lib/toast";
import { useTranslation } from "react-i18next";
import { TSurveyQuota } from "@forma/types/quota";
import type {
  TSurvey,
  TSurveyEndScreenCard,
  TSurveyEnding,
  TSurveyRedirectUrlCard,
} from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { TUserLocale } from "@forma/types/user";
import { cn } from "../../../lib/cn";
import { extractLanguageCodes } from "../../../lib/survey/i18n";
import { recallToHeadline } from "../../../lib/survey/recall";
import { EditorCardMenu } from "./editor-card-menu";
import { EndScreenForm } from "./end-screen-form";
import { RedirectUrlForm } from "./redirect-url-form";
import { getEndingCardTypeChangePatch } from "../lib/ending-card";
import { findEndingCardUsedInLogic, formatTextWithSlashes, isUsedInQuota } from "../lib/utils";
import { ConfirmationModal } from "../../../components/ui/confirmation-modal";
import { OptionsSwitch } from "../../../components/ui/options-switch";

interface EditEndingCardProps {
  localSurvey: TSurvey;
  endingCard: TSurveyEnding;
  endingCardIndex: number;
  setLocalSurvey: React.Dispatch<React.SetStateAction<TSurvey>>;
  setActiveElementId: (id: string | null) => void;
  activeElementId: string | null;
  isInvalid: boolean;
  addEndingCard: (index: number) => void;
  locale: TUserLocale;
  quotas: TSurveyQuota[];
}

export const EditEndingCard = ({
  localSurvey,
  endingCard,
  endingCardIndex,
  setLocalSurvey,
  setActiveElementId,
  activeElementId,
  isInvalid,
  addEndingCard,
  locale,
  quotas,
}: EditEndingCardProps) => {
  const selectedLanguageCode = "default";
  const { t } = useTranslation();

  const [openDeleteConfirmationModal, setOpenDeleteConfirmationModal] = useState(false);

  const endingCardTypes = [
    { value: "endScreen", label: t("workspace.surveys.edit.ending_card") },
    { value: "redirectToUrl", label: t("workspace.surveys.edit.redirect_to_url") },
  ];

  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: endingCard.id,
  });

  let open = activeElementId === endingCard.id;

  const setOpen = (e: boolean) => {
    if (e) {
      setActiveElementId(endingCard.id);
    } else {
      setActiveElementId(null);
    }
  };

  const updateSurvey = (
    data: Partial<TSurveyEndScreenCard & { _forceUpdate?: boolean }> | Partial<TSurveyRedirectUrlCard>
  ) => {
    setLocalSurvey((prevSurvey) => {
      const currentEnding = prevSurvey.endings[endingCardIndex];

      // If subheader was explicitly deleted (is undefined) in the current state,
      // block ALL attempts to recreate it (from Editor cleanup/updates)
      // UNLESS it's a forced update from the "Add Description" button
      const isForceUpdate = "_forceUpdate" in data;
      const { _forceUpdate: _flag, ...filteredData } = data as Partial<
        TSurveyEndScreenCard & { _forceUpdate?: boolean }
      >;

      if (!isForceUpdate && currentEnding?.type === "endScreen" && currentEnding.subheader === undefined) {
        if ("subheader" in filteredData) {
          // Block subheader updates when it's been deleted (Editor cleanup trying to recreate)
          delete filteredData.subheader;
        }
      }

      const updatedEndings = prevSurvey.endings.map((ending, idx) =>
        idx === endingCardIndex ? ({ ...ending, ...filteredData } as TSurveyEnding) : ending
      );
      return { ...prevSurvey, endings: updatedEndings };
    });
  };

  const deleteEndingCard = () => {
    const quota = quotas.find((quota) => isUsedInQuota(quota, { endingCardId: endingCard.id }));
    if (quota) {
      toast.error(
        t("workspace.surveys.edit.ending_used_in_quota", {
          quotaName: quota.name,
        })
      );
      return;
    }
    const isEndingCardUsedInFollowUps = localSurvey.followUps.some((followUp) => {
      if (followUp.trigger.type === "endings") {
        if (followUp.trigger.properties?.endingIds?.includes(endingCard.id)) {
          return true;
        }
      }

      return false;
    });

    // checking if this ending card is used in logic
    const quesIdx = findEndingCardUsedInLogic(localSurvey, endingCard.id);

    if (quesIdx !== -1) {
      toast.error(t("workspace.surveys.edit.ending_card_used_in_logic", { questionIndex: quesIdx + 1 }));
      return;
    }

    if (isEndingCardUsedInFollowUps) {
      setOpenDeleteConfirmationModal(true);
      return;
    }

    setLocalSurvey((prevSurvey) => {
      const updatedEndings = prevSurvey.endings.filter((_, index) => index !== endingCardIndex);
      return { ...prevSurvey, endings: updatedEndings };
    });
  };

  const endScreenHeadline =
    endingCard.type === "endScreen" && endingCard.headline
      ? recallToHeadline(endingCard.headline, localSurvey, true, selectedLanguageCode)[selectedLanguageCode]
      : undefined;

  const style = {
    transition: transition ?? "transform 100ms ease",
    transform: CSS.Translate.toString(transform),
    zIndex: isDragging ? 10 : 1,
  };

  const duplicateEndingCard = () => {
    setLocalSurvey((prevSurvey) => {
      const endingToDuplicate = prevSurvey.endings[endingCardIndex];
      if (!endingToDuplicate) return prevSurvey;
      const duplicatedEndingCard = {
        ...endingToDuplicate,
        id: createId(),
      };
      const updatedEndings = [
        ...prevSurvey.endings.slice(0, endingCardIndex + 1),
        duplicatedEndingCard,
        ...prevSurvey.endings.slice(endingCardIndex + 1),
      ];
      return { ...prevSurvey, endings: updatedEndings };
    });
  };

  const moveEndingCard = (index: number, up: boolean) => {
    setLocalSurvey((prevSurvey) => {
      const newEndings = [...prevSurvey.endings];
      const [movedEnding] = newEndings.splice(index, 1);
      if (!movedEnding) return prevSurvey;
      newEndings.splice(up ? index - 1 : index + 1, 0, movedEnding);
      return { ...prevSurvey, endings: newEndings };
    });
  };

  return (
    <div
      className={cn(open ? "shadow-lg" : "shadow-md", "group z-20 flex flex-row rounded-lg bg-white")}
      ref={setNodeRef}
      style={style}
      id={endingCard.id}>
      {/* The rail is the drag handle, as on the blocks; the grip only shows it. */}
      <div
        {...listeners}
        {...attributes}
        aria-label={t("workspace.surveys.edit.reorder_ending_card", { number: endingCardIndex + 1 })}
        className={cn(
          open ? "bg-slate-50" : "",
          "flex w-10 flex-col items-center justify-between rounded-l-lg border-t border-b border-l py-2 group-aria-expanded:rounded-bl-none",
          "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-slate-700",
          isInvalid ? "bg-red-400" : "bg-white group-hover:bg-slate-50"
        )}>
        <div className="mt-3 flex w-full justify-center">
          {endingCard.type === "endScreen" ? (
            <Handshake className="size-4" />
          ) : (
            <Undo2 className="size-4 rotate-180" />
          )}
        </div>
        <GripIcon
          aria-hidden="true"
          className="size-4 opacity-0 transition-all duration-300 group-hover:opacity-100 group-focus-within:opacity-100"
        />
      </div>
      <Collapsible.Root
        open={open}
        onOpenChange={setOpen}
        className="flex-1 rounded-r-lg border border-slate-200 transition-all duration-300 ease-in-out">
        {/* The trigger holds the title only; the card menu sits beside it, not inside the button. */}
        <div className="flex justify-between gap-4 rounded-r-lg p-5 hover:bg-slate-50">
          <Collapsible.CollapsibleTrigger className="min-w-0 grow cursor-pointer rounded-md text-left focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-slate-700">
            <div className="inline-flex">
              <div>
                <p className="text-sm font-semibold">
                  {endingCard.type === "endScreen" &&
                    (endScreenHeadline
                      ? formatTextWithSlashes(getTextContent(endScreenHeadline))
                      : t("workspace.surveys.edit.ending_card"))}
                  {endingCard.type === "redirectToUrl" &&
                    (endingCard.label || t("workspace.surveys.edit.redirect_to_url"))}
                </p>
                {!open && (
                  <p className="mt-1 truncate text-xs text-slate-500">
                    {endingCard.type === "endScreen"
                      ? t("workspace.surveys.edit.ending_card")
                      : t("workspace.surveys.edit.redirect_to_url")}
                  </p>
                )}
              </div>
            </div>
          </Collapsible.CollapsibleTrigger>

          <div className="flex items-center gap-x-4">
            <EditorCardMenu
              survey={localSurvey}
              cardIdx={endingCardIndex}
              lastCard={endingCardIndex === localSurvey.endings.length - 1}
              duplicateCard={duplicateEndingCard}
              deleteCard={deleteEndingCard}
              moveCard={moveEndingCard}
              card={endingCard}
              updateCard={() => {}}
              addEndingCard={addEndingCard}
              cardType="ending"
            />
          </div>
        </div>
        <Collapsible.CollapsibleContent className={`flex flex-col px-4 ${open && "mt-3 pb-6"}`}>
          <OptionsSwitch
            options={endingCardTypes}
            currentOption={endingCard.type}
            handleOptionChange={(newType) => {
              updateSurvey(
                getEndingCardTypeChangePatch(
                  endingCard,
                  newType === "redirectToUrl" ? "redirectToUrl" : "endScreen",
                  extractLanguageCodes(localSurvey.languages)
                )
              );
            }}
          />
          {endingCard.type === "endScreen" && (
            <EndScreenForm
              localSurvey={localSurvey}
              endingCardIndex={endingCardIndex}
              isInvalid={isInvalid}
              selectedLanguageCode={selectedLanguageCode}
              updateSurvey={updateSurvey}
              endingCard={endingCard}
              locale={locale}
            />
          )}
          {endingCard.type === "redirectToUrl" && (
            <RedirectUrlForm localSurvey={localSurvey} endingCard={endingCard} updateSurvey={updateSurvey} />
          )}
        </Collapsible.CollapsibleContent>
      </Collapsible.Root>

      <ConfirmationModal
        buttonText={t("common.delete")}
        onConfirm={() => {
          setLocalSurvey((prevSurvey) => {
            const updatedEndings = prevSurvey.endings.filter((_, index) => index !== endingCardIndex);
            const surveyFollowUps = prevSurvey.followUps.map((f) => {
              if (f.trigger.properties?.endingIds?.includes(endingCard.id)) {
                return {
                  ...f,
                  trigger: {
                    ...f.trigger,
                    properties: {
                      ...f.trigger.properties,
                      endingIds: f.trigger.properties.endingIds.filter((id) => id !== endingCard.id),
                    },
                  },
                };
              }

              return f;
            });

            return { ...prevSurvey, endings: updatedEndings, followUps: surveyFollowUps };
          });
        }}
        open={openDeleteConfirmationModal}
        setOpen={setOpenDeleteConfirmationModal}
        body={t("workspace.surveys.edit.follow_ups_ending_card_delete_modal_text")}
        title={t("workspace.surveys.edit.follow_ups_ending_card_delete_modal_title")}
      />
    </div>
  );
};
