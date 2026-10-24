import { useSortable } from "@dnd-kit/sortable";
import type { TElementUpdate } from "../types/editor";
import { CSS } from "@dnd-kit/utilities";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import * as Collapsible from "@radix-ui/react-collapsible";
import { ChevronDownIcon, ChevronRightIcon, GripIcon } from "lucide-react";
import { type KeyboardEvent, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TWorkspace as Workspace } from "@forma/types/workspace";
import { TI18nString } from "@forma/types/i18n";
import { TSurveyBlock, TSurveyBlockLogic } from "@forma/types/surveys/blocks";
import { TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";
import { TUserLocale } from "@forma/types/user";
import { cn } from "../../../lib/cn";
import { recallToHeadline } from "../../../lib/survey/recall";
import { AddElementToBlockButton } from "./add-element-to-block-button";
import { AddressElementForm } from "./address-element-form";
import { AdvancedSettings } from "./advanced-settings";
import { BlockMenu } from "./block-menu";
import { BlockSettings } from "./block-settings";
import { CalElementForm } from "./cal-element-form";
import { CESElementForm } from "./ces-element-form";
import { ConditionalLogic } from "./conditional-logic";
import { ConsentElementForm } from "./consent-element-form";
import { ContactInfoElementForm } from "./contact-info-element-form";
import { CSATElementForm } from "./csat-element-form";
import { CTAElementForm } from "./cta-element-form";
import { DateElementForm } from "./date-element-form";
import { EditorCardMenu } from "./editor-card-menu";
import { FileUploadElementForm } from "./file-upload-element-form";
import { MatrixElementForm } from "./matrix-element-form";
import { MultipleChoiceElementForm } from "./multiple-choice-element-form";
import { NPSElementForm } from "./nps-element-form";
import { OpenElementForm } from "./open-element-form";
import { PictureSelectionForm } from "./picture-selection-form";
import { RankingElementForm } from "./ranking-element-form";
import { RatingElementForm } from "./rating-element-form";
import { BLOCK_NAME_MAX_LENGTH } from "../lib/blocks";
import { formatTextWithSlashes } from "../lib/utils";
import { getElementIconMap, getTSurveyElementTypeEnumName } from "../lib/elements";
import { Input } from "../../../components/ui/input";

interface BlockCardProps {
  localSurvey: TSurvey;
  workspace: Workspace;
  block: TSurveyBlock;
  blockIdx: number;
  moveElement: (elementIdx: number, up: boolean) => void;
  updateElement: (elementIdx: number, updatedAttributes: TElementUpdate) => void;
  updateBlockLogic: (elementIdx: number, logic: TSurveyBlockLogic[]) => void;
  updateBlockLogicFallback: (elementIdx: number, logicFallback: string | undefined) => void;
  updateBlockName: (blockIdx: number, name: string) => void;
  updateBlockButtonLabel: (
    blockIndex: number,
    labelKey: "buttonLabel" | "backButtonLabel",
    labelValue: TI18nString | undefined
  ) => void;
  deleteElement: (elementIdx: number) => void;
  duplicateElement: (elementIdx: number) => void;
  activeElementId: string | null;
  setActiveElementId: (elementId: string | null) => void;
  lastElement: boolean;
  lastElementIndex: number;
  invalidElements?: string[];
  addElement: (element: TSurveyElement, index?: number) => void;
  isCxMode: boolean;
  locale: TUserLocale;
  setLocalSurvey: (survey: TSurvey) => void;
  duplicateBlock: (blockId: string) => void;
  deleteBlock: (blockId: string) => void;
  moveBlock: (blockId: string, direction: "up" | "down") => void;
  addElementToBlock: (element: TSurveyElement, blockId: string, afterElementIdx: number) => void;
  moveElementToBlock?: (elementId: string, targetBlockId: string) => void;
  totalBlocks: number;
}

export const BlockCard = ({
  localSurvey,
  workspace,
  block,
  blockIdx,
  moveElement,
  updateElement,
  updateBlockLogic,
  updateBlockLogicFallback,
  updateBlockName,
  updateBlockButtonLabel,
  duplicateElement,
  deleteElement,
  activeElementId,
  setActiveElementId,
  lastElement,
  lastElementIndex,
  invalidElements,
  addElement,
  isCxMode,
  locale,
  setLocalSurvey,
  duplicateBlock,
  deleteBlock,
  moveBlock,
  addElementToBlock,
  moveElementToBlock,
  totalBlocks,
}: Readonly<BlockCardProps>) => {
  const selectedLanguageCode = "default";

  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: block.id,
  });
  const { t } = useTranslation();
  const ELEMENTS_ICON_MAP = getElementIconMap(t);

  const hasMultipleElements = block.elements.length > 1;
  const blockLogic = block.logic ?? [];

  // Check if any element in this block is currently active
  const isBlockOpen = block.elements.some((element) => element.id === activeElementId);

  const hasInvalidElement = block.elements.some((element) => invalidElements?.includes(element.id));
  const hasInvalidLogic = blockLogic.some((logicItem) => invalidElements?.includes(logicItem.id));
  // The block's own id is only ever flagged for an empty name (elements and logic rules use theirs).
  const hasInvalidName = invalidElements?.includes(block.id) ?? false;
  const isBlockInvalid = hasInvalidElement || hasInvalidLogic || hasInvalidName;

  const [isBlockCollapsed, setIsBlockCollapsed] = useState(false);
  const [openAdvanced, setOpenAdvanced] = useState(blockLogic.length > 0);

  const nameInputRef = useRef<HTMLInputElement>(null);

  // Enter commits the rename by blurring, so the title reads as settled. There is nothing to flush:
  // the name is already in localSurvey and rides the editor's existing autosave.
  const handleNameKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    // An IME (Japanese/Chinese/Korean) uses Enter to accept the highlighted candidate, so blurring
    // there would commit a half-typed name. The synthetic event doesn't carry isComposing; the
    // native one does.
    if (event.key !== "Enter" || event.nativeEvent.isComposing) return;
    event.preventDefault();
    nameInputRef.current?.blur();
  };

  const [elementsParent] = useAutoAnimate();

  const getElementHeadline = (
    element: TSurveyElement,
    languageCode: string
  ): (string | React.ReactElement)[] | string | undefined => {
    const headlineData = recallToHeadline(element.headline, localSurvey, true, languageCode);
    const headlineText = headlineData[languageCode];
    if (headlineText) {
      return formatTextWithSlashes(getTextContent(headlineText ?? ""));
    }
    return getTSurveyElementTypeEnumName(element.type, t);
  };

  // Common props shared by all element forms
  const getCommonFormProps = (element: TSurveyElement, elementIdx: number) => ({
    localSurvey,
    element,
    elementIdx,
    updateElement,
    selectedLanguageCode,
    isInvalid: invalidElements ? invalidElements.includes(element.id) : false,
    locale,
  });

  // Element form components mapped by type
  const elementFormMap = {
    [TSurveyElementTypeEnum.OpenText]: OpenElementForm,
    [TSurveyElementTypeEnum.MultipleChoiceSingle]: MultipleChoiceElementForm,
    [TSurveyElementTypeEnum.MultipleChoiceMulti]: MultipleChoiceElementForm,
    [TSurveyElementTypeEnum.NPS]: NPSElementForm,
    [TSurveyElementTypeEnum.CTA]: CTAElementForm,
    [TSurveyElementTypeEnum.Rating]: RatingElementForm,
    [TSurveyElementTypeEnum.CSAT]: CSATElementForm,
    [TSurveyElementTypeEnum.CES]: CESElementForm,
    [TSurveyElementTypeEnum.Consent]: ConsentElementForm,
    [TSurveyElementTypeEnum.Date]: DateElementForm,
    [TSurveyElementTypeEnum.PictureSelection]: PictureSelectionForm,
    [TSurveyElementTypeEnum.FileUpload]: FileUploadElementForm,
    [TSurveyElementTypeEnum.Cal]: CalElementForm,
    [TSurveyElementTypeEnum.Matrix]: MatrixElementForm,
    [TSurveyElementTypeEnum.Address]: AddressElementForm,
    [TSurveyElementTypeEnum.Ranking]: RankingElementForm,
    [TSurveyElementTypeEnum.ContactInfo]: ContactInfoElementForm,
  };

  // Elements that need lastElement prop
  const elementsWithLastElement = new Set([
    TSurveyElementTypeEnum.OpenText,
    TSurveyElementTypeEnum.CTA,
    TSurveyElementTypeEnum.Rating,
    TSurveyElementTypeEnum.Cal,
    TSurveyElementTypeEnum.ContactInfo,
  ]);

  const renderElementForm = (element: TSurveyElement, elementIdx: number) => {
    const FormComponent = elementFormMap[element.type];
    if (!FormComponent) return null;

    const commonProps = getCommonFormProps(element, elementIdx);

    // Add lastElement for specific element types
    const additionalProps: Record<string, unknown> = {};
    if (elementsWithLastElement.has(element.type)) {
      additionalProps.lastElement = lastElement;
    }

    // @ts-expect-error - These props should cover everything
    return <FormComponent {...commonProps} {...additionalProps} />;
  };

  const style = {
    transition: transition ?? "transform 100ms ease",
    transform: CSS.Translate.toString(transform),
    zIndex: isDragging ? 10 : 1,
  };

  let blockSidebarColorClass = "";
  if (isBlockInvalid) {
    blockSidebarColorClass = "bg-red-400";
  } else {
    blockSidebarColorClass = isBlockOpen ? "bg-slate-700" : "bg-slate-400";
  }

  return (
    <div
      className={cn(
        isBlockOpen ? "shadow-lg" : "shadow-md",
        // scroll-mt clears the fixed tabs bar (h-12) when scrolled into view on validation errors
        "flex w-full scroll-mt-16 flex-row rounded-lg bg-white duration-300"
      )}
      ref={setNodeRef}
      style={style}
      id={block.id}>
      {/* The rail is the drag handle (dnd-kit gives it the button role and keyboard reordering); the grip only shows it, on hover or keyboard focus. */}
      <div
        {...listeners}
        {...attributes}
        aria-label={t("workspace.surveys.edit.reorder_block", { number: blockIdx + 1 })}
        className={cn(
          blockSidebarColorClass,
          "group top-0 w-10 rounded-l-lg p-2 text-center text-sm text-white hover:cursor-grab hover:bg-slate-600",
          "flex flex-col items-center justify-between gap-2",
          "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-slate-700"
        )}>
        <div className="mt-3 flex w-full items-center justify-center rounded-full bg-white p-1 text-xs font-medium text-slate-900">
          {blockIdx + 1}
        </div>

        <GripIcon
          aria-hidden="true"
          className="size-4 opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100"
        />
      </div>
      <div className="flex-1 rounded-r-lg border border-slate-200">
        <Collapsible.Root
          open={!isBlockCollapsed}
          onOpenChange={() => setIsBlockCollapsed(!isBlockCollapsed)}
          className={cn(isBlockCollapsed ? "h-full" : "")}>
          <div
            className="relative flex h-full w-full items-center justify-between rounded-tr-lg px-4 py-2 hover:bg-slate-100"
            data-testid="block-card-header">
            {/* The whole header folds the block, as before, but the trigger is a real button stretched behind the row, so the name field and the menu sit beside it instead of inside it, and the block can be folded from the keyboard. */}
            <Collapsible.CollapsibleTrigger
              aria-labelledby={`${block.id}-name ${block.id}-count`}
              className="absolute inset-0 cursor-pointer rounded-tr-lg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-slate-700"
            />
            <div className="pointer-events-none relative flex min-w-0 items-center gap-2">
              <div className="min-w-0">
                <Input
                  id={`${block.id}-name`}
                  ref={nameInputRef}
                  // Controlled: an auto-generated name is resequenced in place by a reorder,
                  // and an uncontrolled field would keep showing the number it mounted with.
                  value={block.name}
                  placeholder={t("workspace.surveys.edit.block_name")}
                  aria-label={t("workspace.surveys.edit.block_name")}
                  maxLength={BLOCK_NAME_MAX_LENGTH}
                  isInvalid={hasInvalidName}
                  onChange={(e) => updateBlockName(blockIdx, e.target.value)}
                  onKeyDown={handleNameKeyDown}
                  className={cn(
                    "pointer-events-auto -mx-2 h-6 w-full rounded-md border-transparent bg-transparent px-2 py-0",
                    // The placeholder is muted on purpose: a cleared name is an invalid state
                    // that must not look like a block still called something.
                    "text-sm font-medium text-slate-700 placeholder:font-normal placeholder:text-slate-400",
                    // Wide enough to type a real title into without the box clipping the text,
                    // and capped so a long one can't crowd out the block menu.
                    "max-w-[34rem] min-w-[20rem]",
                    // Dashed hover/focus box, matching the other inline renames in the app
                    // (see workflow-page-title.tsx): slate while hovered, brand while editing.
                    "border border-dashed transition-colors",
                    "hover:border-slate-300 focus:border-brand-dark",
                    // Same specificity means source order decides, and Tailwind emits hover
                    // last — without this the border drops to slate on a hovered focused field.
                    "focus:hover:border-brand-dark",
                    "focus:ring-0 focus:ring-offset-0 focus:outline-none"
                  )}
                />
                <p id={`${block.id}-count`} className="text-xs text-slate-500">
                  {t("common.count_questions", { count: block.elements.length })}
                </p>
              </div>
            </div>
            <div className="relative">
              <BlockMenu
                isFirstBlock={blockIdx === 0}
                isLastBlock={blockIdx === totalBlocks - 1}
                isOnlyBlock={totalBlocks === 1}
                onDuplicate={() => duplicateBlock(block.id)}
                onDelete={() => deleteBlock(block.id)}
                onMoveUp={() => moveBlock(block.id, "up")}
                onMoveDown={() => moveBlock(block.id, "down")}
              />
            </div>
          </div>

          <Collapsible.CollapsibleContent>
            {/* Render each element in the block */}
            <div ref={elementsParent}>
              {block.elements.map((element, elementIndex) => {
                // Calculate the actual element index in the flattened elements array
                let elementIdx = 0;
                for (const previousBlock of localSurvey.blocks.slice(0, blockIdx)) {
                  elementIdx += previousBlock.elements.length;
                }
                elementIdx += elementIndex;

                const isOpen = activeElementId === element.id;

                return (
                  <div
                    key={element.id}
                    id={element.id}
                    className={cn("scroll-mt-16", elementIndex > 0 && "border-t border-slate-200")}>
                    <Collapsible.Root
                      open={isOpen}
                      onOpenChange={() => {
                        if (activeElementId !== element.id) {
                          setActiveElementId(element.id);
                        } else {
                          setActiveElementId(null);
                        }
                      }}
                      className="w-full">
                      <div
                        className={cn(
                          isOpen ? "bg-slate-50" : "",
                          "flex w-full items-center justify-between gap-4 p-4 hover:bg-slate-50"
                        )}>
                        {/* The heading names the question; the button inside it opens and closes it (the accordion pattern), with the card menu beside it rather than inside the button. */}
                        <h3 className="min-w-0 grow">
                          <Collapsible.CollapsibleTrigger
                            className="flex w-full cursor-pointer items-center gap-3 rounded-md text-left focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-slate-700"
                            dir="auto">
                            <span className="flex items-center text-slate-600" aria-hidden="true">
                              {ELEMENTS_ICON_MAP[element.type]}
                            </span>
                            <span className="flex min-w-0 grow flex-col justify-center">
                              {hasMultipleElements && (
                                <span className="mb-1 text-xs font-medium text-slate-500">
                                  {t("workspace.surveys.edit.question_number", {
                                    number: elementIndex + 1,
                                  })}
                                </span>
                              )}
                              <span className="text-sm font-semibold">
                                {getElementHeadline(element, selectedLanguageCode)}
                              </span>
                              {!isOpen && element.type !== TSurveyElementTypeEnum.CTA && (
                                <span className="mt-1 truncate text-xs font-normal text-slate-500">
                                  {element?.required
                                    ? t("workspace.surveys.edit.required")
                                    : t("workspace.surveys.edit.optional")}
                                </span>
                              )}
                            </span>
                          </Collapsible.CollapsibleTrigger>
                        </h3>

                        <div className="flex items-center gap-x-2">
                          <EditorCardMenu
                            survey={localSurvey}
                            cardIdx={elementIdx}
                            lastCard={lastElement && elementIndex === lastElementIndex}
                            blockId={block.id}
                            elementIdx={elementIndex}
                            duplicateCard={duplicateElement}
                            deleteCard={deleteElement}
                            moveCard={moveElement}
                            card={{
                              ...element,
                              logic: block.logic,
                              buttonLabel: block.buttonLabel,
                              backButtonLabel: block.backButtonLabel,
                            }}
                            workspace={workspace}
                            updateCard={updateElement}
                            addCard={addElement}
                            addCardToBlock={addElementToBlock}
                            moveElementToBlock={moveElementToBlock}
                            cardType="element"
                            isCxMode={isCxMode}
                          />
                        </div>
                      </div>
                      <Collapsible.CollapsibleContent className={`flex flex-col px-4 ${isOpen && "pb-4"}`}>
                        {renderElementForm(element, elementIdx)}
                        <div className="mt-4">
                          <Collapsible.Root
                            open={openAdvanced}
                            onOpenChange={setOpenAdvanced}
                            className="mt-5">
                            <Collapsible.CollapsibleTrigger
                              className="flex items-center text-sm text-slate-700"
                              aria-label="Toggle advanced settings">
                              {openAdvanced ? (
                                <ChevronDownIcon className="mr-1 h-4 w-3" />
                              ) : (
                                <ChevronRightIcon className="mr-2 h-4 w-3" />
                              )}
                              {openAdvanced
                                ? t("workspace.surveys.edit.hide_question_settings")
                                : t("workspace.surveys.edit.show_question_settings")}
                            </Collapsible.CollapsibleTrigger>

                            <Collapsible.CollapsibleContent className="data-[state=closed]:animate-collapsible-up data-[state=open]:animate-collapsible-down flex flex-col gap-4 overflow-hidden">
                              {element.type !== TSurveyElementTypeEnum.NPS &&
                              element.type !== TSurveyElementTypeEnum.Rating &&
                              element.type !== TSurveyElementTypeEnum.CTA ? (
                                <div className="mt-2 flex gap-x-2"></div>
                              ) : null}
                              <AdvancedSettings
                                // TODO -- We should remove this when we can confirm that everything works fine with the survey editor, not changing this right now in this file because it would require changing the element type to the respective element type in all the element forms.
                                element={element}
                                elementIdx={elementIdx}
                                localSurvey={localSurvey}
                                updateElement={updateElement}
                                updateBlockLogic={updateBlockLogic}
                                updateBlockLogicFallback={updateBlockLogicFallback}
                              />
                            </Collapsible.CollapsibleContent>
                          </Collapsible.Root>
                        </div>
                      </Collapsible.CollapsibleContent>
                    </Collapsible.Root>
                  </div>
                );
              })}
            </div>
            <hr className="mb-4 border-dashed border-slate-200" />
            {/* Add Element to Block button */}

            <div className="p-4 pt-0">
              <AddElementToBlockButton
                localSurvey={localSurvey}
                setLocalSurvey={setLocalSurvey}
                setActiveElementId={setActiveElementId}
                block={block}
                workspace={workspace}
                isCxMode={isCxMode}
              />
            </div>

            <hr className="border-dashed border-slate-200" />

            {/* Conditional Logic */}
            {block.elements[0] && (
              <div className="p-4 pb-0">
                <ConditionalLogic
                  localSurvey={localSurvey}
                  block={block}
                  blockIdx={blockIdx}
                  updateBlockLogic={updateBlockLogic}
                  updateBlockLogicFallback={updateBlockLogicFallback}
                  invalidElements={invalidElements}
                />
              </div>
            )}

            {/* Block Settings */}
            <div className="p-4">
              <BlockSettings
                localSurvey={localSurvey}
                block={block}
                blockIndex={blockIdx}
                selectedLanguageCode={selectedLanguageCode}
                updateBlockButtonLabel={updateBlockButtonLabel}
                locale={locale}
                isLastBlock={blockIdx === totalBlocks - 1}
              />
            </div>
          </Collapsible.CollapsibleContent>
        </Collapsible.Root>
      </div>
    </div>
  );
};
