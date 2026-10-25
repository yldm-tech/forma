import { DndContext, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { createId } from "@paralleldrive/cuid2";
import { PlusIcon } from "lucide-react";
import { type JSX, useCallback } from "react";
import { toast } from "../../../lib/toast";
import { useTranslation } from "react-i18next";
import { TI18nString } from "@forma/types/i18n";
import { type TSurveyMatrixElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { createI18nString, extractLanguageCodes } from "../../../lib/survey/i18n";
import type { TElementUpdate, TUpdateElement } from "../types/editor";
import { ElementFormInput } from "./element-form-input/index";
import { MatrixSortableItem } from "./matrix-sortable-item";
import { ValidationRulesEditor } from "./validation-rules-editor";
import { findOptionUsedInLogic } from "../lib/utils";
import { Button } from "../../../components/ui/button";
import { Label } from "../../../components/ui/label";
import { ShuffleOptionSelect } from "../../../components/ui/shuffle-option-select";
import { isLabelValidForAllLanguages } from "../lib/validation";

interface MatrixElementFormProps {
  localSurvey: TSurvey;
  element: TSurveyMatrixElement;
  elementIdx: number;
  updateElement: TUpdateElement;
  isInvalid: boolean;
  locale: TUserLocale;
}

export const MatrixElementForm = ({
  element,
  elementIdx,
  updateElement,
  isInvalid,
  localSurvey,
  locale,
}: MatrixElementFormProps): JSX.Element => {
  const languageCodes = extractLanguageCodes(localSurvey.languages);
  const { t } = useTranslation();

  const focusItem = (targetIdx: number, type: "row" | "column") => {
    const input = document.querySelector(`input[id="${type}-${targetIdx}"]`) as HTMLInputElement;
    if (input) input.focus();
  };

  // Row and column edits are computed from the element's latest rows and columns, not this render's: a label is committed 100 ms after its last keystroke, after Enter may already have added the next one.
  const updateCurrentElement = (compute: (current: TSurveyMatrixElement) => TElementUpdate) =>
    updateElement(elementIdx, (current) =>
      current.type === TSurveyElementTypeEnum.Matrix ? compute(current) : {}
    );

  // Function to add a new Label input field
  const handleAddLabel = (type: "row" | "column") => {
    // Created outside the updater, which React may call more than once.
    const newLabel = { id: createId(), label: createI18nString("", languageCodes) };
    if (type === "row") {
      updateCurrentElement((current) => ({ rows: [...current.rows, newLabel] }));
      setTimeout(() => focusItem(element.rows.length, type), 0);
    } else {
      updateCurrentElement((current) => ({ columns: [...current.columns, newLabel] }));
      setTimeout(() => focusItem(element.columns.length, type), 0);
    }
  };

  // Function to delete a label input field
  const handleDeleteLabel = (type: "row" | "column", index: number) => {
    const labels = type === "row" ? element.rows : element.columns;
    if (labels.length <= 2) return; // Prevent deleting below minimum length

    // check if the label is used in logic
    if (type === "column") {
      const elementIdx = findOptionUsedInLogic(localSurvey, element.id, index.toString());
      if (elementIdx !== -1) {
        toast.error(
          t("workspace.surveys.edit.column_used_in_logic_error", {
            questionIndex: elementIdx + 1,
          })
        );
        return;
      }
    } else {
      const elementIdx = findOptionUsedInLogic(localSurvey, element.id, index.toString(), true);
      if (elementIdx !== -1) {
        toast.error(
          t("workspace.surveys.edit.row_used_in_logic_error", {
            questionIndex: elementIdx + 1,
          })
        );
        return;
      }
    }

    const labelId = labels[index]?.id;
    if (!labelId) return;

    if (type === "row") {
      updateCurrentElement((current) => ({ rows: current.rows.filter((row) => row.id !== labelId) }));
    } else {
      updateCurrentElement((current) => ({
        columns: current.columns.filter((column) => column.id !== labelId),
      }));
    }
  };

  const updateMatrixLabel = (index: number, type: "row" | "column", matrixLabel: TI18nString) => {
    // The index is this render's; the row or column it names is found by id in the latest ones. A copy is written: the label objects belong to the editor's state.
    const labelId = (type === "row" ? element.rows : element.columns)[index]?.id;
    // Without a label at the index a new one is added, created outside the updater, which React may call more than once.
    const newLabel = { id: createId(), label: matrixLabel };
    const apply = (labels: TSurveyMatrixElement["rows"]) =>
      labelId
        ? labels.map((label) => (label.id === labelId ? { ...label, label: matrixLabel } : label))
        : [...labels, newLabel];

    if (type === "row") {
      updateCurrentElement((current) => ({ rows: apply(current.rows) }));
    } else {
      updateCurrentElement((current) => ({ columns: apply(current.columns) }));
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent, type: "row" | "column", currentIndex: number) => {
    const items = type === "row" ? element.rows : element.columns;

    if (e.key === "Enter") {
      e.preventDefault();
      if (currentIndex === items.length - 1) {
        handleAddLabel(type);
      } else {
        focusItem(currentIndex + 1, type);
      }
    }

    if (e.key === "ArrowDown") {
      e.preventDefault();
      if (currentIndex + 1 < items.length) {
        focusItem(currentIndex + 1, type);
      }
    }

    if (e.key === "ArrowUp") {
      e.preventDefault();
      if (currentIndex > 0) {
        focusItem(currentIndex - 1, type);
      }
    }
  };

  const handleMatrixDragEnd = useCallback(
    (type: "row" | "column", event: DragEndEvent) => {
      const { active, over } = event;

      if (!active || !over || active.id === over.id) return;

      const items = type === "row" ? [...element.rows] : [...element.columns];
      const activeIndex = items.findIndex((item) => item.id === active.id);
      const overIndex = items.findIndex((item) => item.id === over.id);

      if (activeIndex === -1 || overIndex === -1) return;

      const movedItem = items[activeIndex];
      if (!movedItem) return;
      items.splice(activeIndex, 1);
      items.splice(overIndex, 0, movedItem);

      updateElement(elementIdx, type === "row" ? { rows: items } : { columns: items });
    },
    [elementIdx, updateElement, element.rows, element.columns]
  );

  const shuffleOptionsTypes = {
    none: {
      id: "none",
      label: t("workspace.surveys.edit.keep_current_order"),
      show: true,
    },
    all: {
      id: "all",
      label: t("workspace.surveys.edit.randomize_all"),
      show: true,
    },
    exceptLast: {
      id: "exceptLast",
      label: t("workspace.surveys.edit.randomize_all_except_last"),
      show: true,
    },
    reverseOrderOccasionally: {
      id: "reverseOrderOccasionally",
      label: t("workspace.surveys.edit.reverse_order_occasionally"),
      show: true,
    },
    reverseOrderExceptLast: {
      id: "reverseOrderExceptLast",
      label: t("workspace.surveys.edit.reverse_order_occasionally_except_last"),
      show: true,
    },
  };
  const [parent] = useAutoAnimate();

  return (
    <form>
      <ElementFormInput
        id="headline"
        value={element.headline}
        label={t("workspace.surveys.edit.question") + "*"}
        localSurvey={localSurvey}
        elementIdx={elementIdx}
        isInvalid={isInvalid}
        updateElement={updateElement}
        locale={locale}
        autoFocus={!element.headline?.default || element.headline.default.trim() === ""}
      />
      <div ref={parent}>
        {element.subheader !== undefined && (
          <div className="inline-flex w-full items-center">
            <div className="w-full">
              <ElementFormInput
                id="subheader"
                value={element.subheader}
                label={t("common.description")}
                localSurvey={localSurvey}
                elementIdx={elementIdx}
                isInvalid={isInvalid}
                updateElement={updateElement}
                locale={locale}
                autoFocus={!element.subheader?.default || element.subheader.default.trim() === ""}
              />
            </div>
          </div>
        )}
        {element.subheader === undefined && (
          <Button
            size="sm"
            variant="secondary"
            className="mt-3"
            type="button"
            onClick={() => {
              updateElement(elementIdx, {
                subheader: createI18nString("", languageCodes),
              });
            }}>
            <PlusIcon className="mr-1 size-4" />
            {t("workspace.surveys.edit.add_description")}
          </Button>
        )}
      </div>
      <div className="mt-3 grid grid-cols-2 gap-4">
        <div>
          {/* Rows section */}
          <Label htmlFor="rows">{t("workspace.surveys.edit.rows")}</Label>
          <div className="mt-2">
            <DndContext id="matrix-rows" onDragEnd={(e) => handleMatrixDragEnd("row", e)}>
              <SortableContext items={element.rows} strategy={verticalListSortingStrategy}>
                <div className="flex flex-col gap-2" ref={parent}>
                  {element.rows.map((row, index) => (
                    <MatrixSortableItem
                      key={row.id}
                      choice={row}
                      index={index}
                      type="row"
                      localSurvey={localSurvey}
                      element={element}
                      elementIdx={elementIdx}
                      updateMatrixLabel={updateMatrixLabel}
                      onDelete={(index) => handleDeleteLabel("row", index)}
                      onKeyDown={(e) => handleKeyDown(e, "row", index)}
                      canDelete={element.rows.length > 2}
                      isInvalid={
                        isInvalid &&
                        !isLabelValidForAllLanguages(element.rows[index]?.label ?? {}, localSurvey.languages)
                      }
                      locale={locale}
                    />
                  ))}
                </div>
              </SortableContext>
            </DndContext>
            <Button
              variant="secondary"
              size="sm"
              className="mt-2 w-fit"
              onClick={(e) => {
                e.preventDefault();
                handleAddLabel("row");
              }}>
              <PlusIcon />
              {t("workspace.surveys.edit.add_row")}
            </Button>
          </div>
        </div>
        <div>
          {/* Columns section */}
          <Label htmlFor="columns">{t("workspace.surveys.edit.columns")}</Label>
          <div className="mt-2">
            <DndContext id="matrix-columns" onDragEnd={(e) => handleMatrixDragEnd("column", e)}>
              <SortableContext items={element.columns} strategy={verticalListSortingStrategy}>
                <div className="flex flex-col gap-2" ref={parent}>
                  {element.columns.map((column, index) => (
                    <MatrixSortableItem
                      key={column.id}
                      choice={column}
                      index={index}
                      type="column"
                      localSurvey={localSurvey}
                      element={element}
                      elementIdx={elementIdx}
                      updateMatrixLabel={updateMatrixLabel}
                      onDelete={(index) => handleDeleteLabel("column", index)}
                      onKeyDown={(e) => handleKeyDown(e, "column", index)}
                      canDelete={element.columns.length > 2}
                      isInvalid={
                        isInvalid &&
                        !isLabelValidForAllLanguages(
                          element.columns[index]?.label ?? {},
                          localSurvey.languages
                        )
                      }
                      locale={locale}
                    />
                  ))}
                </div>
              </SortableContext>
            </DndContext>
            <Button
              variant="secondary"
              size="sm"
              className="mt-2 w-fit"
              onClick={(e) => {
                e.preventDefault();
                handleAddLabel("column");
              }}>
              <PlusIcon />
              {t("workspace.surveys.edit.add_column")}
            </Button>
          </div>
          <div className="mt-3 flex flex-1 items-center justify-end gap-2">
            <ShuffleOptionSelect
              shuffleOptionsTypes={shuffleOptionsTypes}
              elementIdx={elementIdx}
              updateElement={updateElement}
              shuffleOption={element.shuffleOption}
            />
          </div>
        </div>
      </div>
      <ValidationRulesEditor
        elementType={element.type}
        validation={element.validation}
        onUpdateValidation={(validation) => {
          updateElement(elementIdx, {
            validation,
          });
        }}
        element={element}
      />
    </form>
  );
};
