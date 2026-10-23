import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVerticalIcon, TrashIcon } from "lucide-react";
import type { JSX } from "react";
import { useTranslation } from "react-i18next";
import { type TI18nString } from "@forma/types/i18n";
import { TSurveyMatrixElement, TSurveyMatrixElementChoice } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { ElementFormInput } from "./element-form-input/index";
import { Button } from "../../../components/ui/button";
import { TooltipRenderer } from "../../../components/ui/tooltip";

interface MatrixSortableItemProps {
  choice: TSurveyMatrixElementChoice;
  type: "row" | "column";
  index: number;
  localSurvey: TSurvey;
  element: TSurveyMatrixElement;
  elementIdx: number;
  updateMatrixLabel: (index: number, type: "row" | "column", matrixLabel: TI18nString) => void;
  onDelete: (index: number) => void;
  onKeyDown: (e: React.KeyboardEvent) => void;
  canDelete: boolean;
  isInvalid: boolean;
  locale: TUserLocale;
}

export const MatrixSortableItem = ({
  choice,
  type,
  index,
  localSurvey,
  elementIdx,
  updateMatrixLabel,
  onDelete,
  onKeyDown,
  canDelete,
  isInvalid,
  locale,
}: MatrixSortableItemProps): JSX.Element => {
  const { t } = useTranslation();

  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({
    id: choice.id,
  });

  const style = {
    transition: transition ?? "transform 100ms ease",
    transform: CSS.Translate.toString(transform),
  };

  return (
    <div className="flex w-full items-center gap-2" ref={setNodeRef} style={style}>
      <div {...listeners} {...attributes}>
        <GripVerticalIcon className="size-4 cursor-move text-slate-400" />
      </div>

      <div className="flex w-full items-center">
        <ElementFormInput
          key={choice.id}
          id={`${type}-${index}`}
          label=""
          localSurvey={localSurvey}
          elementIdx={elementIdx}
          value={choice.label}
          updateMatrixLabel={updateMatrixLabel}
          isInvalid={isInvalid}
          locale={locale}
          onKeyDown={onKeyDown}
        />
        {canDelete && (
          <TooltipRenderer data-testid="tooltip-renderer" tooltipContent={t("common.delete")}>
            <Button
              variant="ghost"
              size="icon"
              className="ml-2"
              onClick={(e) => {
                e.preventDefault();
                onDelete(index);
              }}>
              <TrashIcon />
            </Button>
          </TooltipRenderer>
        )}
      </div>
    </div>
  );
};
