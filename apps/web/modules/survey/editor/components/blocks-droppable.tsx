import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { Workspace } from "@forma/database/prisma-browser";
import { TI18nString } from "@forma/types/i18n";
import { TSurveyBlockLogic } from "@forma/types/surveys/blocks";
import { TSurveyElement } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { BlockCard } from "@/modules/survey/editor/components/block-card";

interface BlocksDroppableProps {
  localSurvey: TSurvey;
  setLocalSurvey: (survey: TSurvey) => void;
  workspace: Workspace;
  moveElement: (elementIdx: number, up: boolean) => void;
  updateElement: (elementIdx: number, updatedAttributes: any) => void;
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
  invalidElements: string[] | null;
  addElement: (element: any, index?: number) => void;
  isFormaCloud: boolean;
  isCxMode: boolean;
  locale: TUserLocale;
  responseCount: number;
  onAlertTrigger: () => void;
  isStorageConfigured: boolean;
  isExternalUrlsAllowed: boolean;
  duplicateBlock: (blockId: string) => void;
  deleteBlock: (blockId: string) => void;
  moveBlock: (blockId: string, direction: "up" | "down") => void;
  addElementToBlock: (element: TSurveyElement, blockId: string, afterElementIdx: number) => void;
  moveElementToBlock?: (elementId: string, targetBlockId: string) => void;
}

export const BlocksDroppable = ({
  activeElementId,
  deleteElement,
  duplicateElement,
  invalidElements,
  localSurvey,
  setLocalSurvey,
  moveElement,
  workspace,
  setActiveElementId,
  updateElement,
  updateBlockLogic,
  updateBlockLogicFallback,
  updateBlockName,
  updateBlockButtonLabel,
  addElement,
  isFormaCloud,
  isCxMode,
  locale,
  responseCount,
  onAlertTrigger,
  isStorageConfigured = true,
  isExternalUrlsAllowed,
  duplicateBlock,
  deleteBlock,
  moveBlock,
  addElementToBlock,
  moveElementToBlock,
}: Readonly<BlocksDroppableProps>) => {
  const [parent] = useAutoAnimate();

  return (
    <div className="group mb-5 flex w-full flex-col gap-5" ref={parent}>
      <SortableContext items={localSurvey.blocks} strategy={verticalListSortingStrategy}>
        {localSurvey.blocks.map((block, blockIdx) => {
          // Check if this is the last block and has elements
          const isLastBlock = blockIdx === localSurvey.blocks.length - 1;
          const lastElementIndex = block.elements.length - 1;

          return (
            <BlockCard
              key={block.id}
              localSurvey={localSurvey}
              setLocalSurvey={setLocalSurvey}
              workspace={workspace}
              block={block}
              blockIdx={blockIdx}
              moveElement={moveElement}
              updateElement={updateElement}
              updateBlockLogic={updateBlockLogic}
              updateBlockLogicFallback={updateBlockLogicFallback}
              updateBlockName={updateBlockName}
              updateBlockButtonLabel={updateBlockButtonLabel}
              duplicateElement={duplicateElement}
              deleteElement={deleteElement}
              activeElementId={activeElementId}
              setActiveElementId={setActiveElementId}
              lastElement={isLastBlock}
              lastElementIndex={lastElementIndex}
              invalidElements={invalidElements ?? undefined}
              addElement={addElement}
              isFormaCloud={isFormaCloud}
              isCxMode={isCxMode}
              locale={locale}
              responseCount={responseCount}
              onAlertTrigger={onAlertTrigger}
              isStorageConfigured={isStorageConfigured}
              isExternalUrlsAllowed={isExternalUrlsAllowed}
              duplicateBlock={duplicateBlock}
              deleteBlock={deleteBlock}
              moveBlock={moveBlock}
              addElementToBlock={addElementToBlock}
              moveElementToBlock={moveElementToBlock}
              totalBlocks={localSurvey.blocks.length}
            />
          );
        })}
      </SortableContext>
    </div>
  );
};
