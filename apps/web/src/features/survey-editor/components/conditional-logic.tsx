import { useAutoAnimate } from "@formkit/auto-animate/react";
import { createId } from "@paralleldrive/cuid2";
import * as Collapsible from "@radix-ui/react-collapsible";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  CopyIcon,
  EllipsisVerticalIcon,
  PlusIcon,
  SplitIcon,
  TrashIcon,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { TSurveyBlock, TSurveyBlockLogic } from "@forma/types/surveys/blocks";
import { TSurvey } from "@forma/types/surveys/types";
import { cn } from "../../../lib/cn";
import { duplicateLogicItem } from "../lib/survey-logic";
import { logicRuleDomId } from "../lib/survey-issues";
import { replaceHeadlineRecall } from "../../../lib/survey/recall";
import { LogicEditor } from "./logic-editor";
import { getDefaultOperatorForElement, replaceEndingCardHeadlineRecall } from "../lib/utils";
import { Button } from "../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../components/ui/dropdown-menu";
import { Label } from "../../../components/ui/label";

interface ConditionalLogicProps {
  localSurvey: TSurvey;
  blockIdx: number;
  block: TSurveyBlock;
  updateBlockLogic: (blockIdx: number, logic: TSurveyBlockLogic[]) => void;
  updateBlockLogicFallback: (blockIdx: number, logicFallback: string | undefined) => void;
  invalidElements?: string[];
}

export function ConditionalLogic({
  localSurvey,
  blockIdx,
  block,
  updateBlockLogic,
  updateBlockLogicFallback,
  invalidElements,
}: ConditionalLogicProps) {
  const { t } = useTranslation();
  const transformedSurvey = useMemo(() => {
    let modifiedSurvey = replaceHeadlineRecall(localSurvey, "default");
    modifiedSurvey = replaceEndingCardHeadlineRecall(modifiedSurvey, "default");

    return modifiedSurvey;
  }, [localSurvey]);

  const blockLogic = useMemo(() => block.logic ?? [], [block.logic]);
  const blockLogicFallback = block.logicFallback;

  // A logic rule is flagged as invalid on save/publish (see validateSurveyWithZod).
  // Surface it here so the user can locate the offending rule.
  const hasLogicError = useMemo(
    () => blockLogic.some((logicItem) => invalidElements?.includes(logicItem.id)),
    [blockLogic, invalidElements]
  );

  // The logic rules are collapsed by default and expand when the user adds a rule.
  const [isOpen, setIsOpen] = useState(false);

  // Use the first element in the block as the reference element for default operators
  const firstElement = block.elements[0];

  const addLogic = () => {
    if (!firstElement) return;

    const operator = getDefaultOperatorForElement(firstElement, t);

    const initialCondition: TSurveyBlockLogic = {
      id: createId(),
      conditions: {
        id: createId(),
        connector: "and",
        conditions: [
          {
            id: createId(),
            leftOperand: {
              value: firstElement.id,
              type: "element",
            },
            operator,
          },
        ],
      },
      actions: [
        {
          id: createId(),
          objective: "jumpToBlock",
          target: "",
        },
      ],
    };

    updateBlockLogic(blockIdx, [...blockLogic, initialCondition]);
    // Expand the section so the freshly added rule is visible and editable.
    setIsOpen(true);
  };

  const handleRemoveLogic = (logicItemIdx: number) => {
    const logicCopy = structuredClone(blockLogic);
    const isLast = logicCopy.length === 1;
    logicCopy.splice(logicItemIdx, 1);

    updateBlockLogic(blockIdx, logicCopy);
    if (isLast) {
      updateBlockLogicFallback(blockIdx, undefined);
    }
  };

  const moveLogic = (from: number, to: number) => {
    const logicCopy = structuredClone(blockLogic);
    const [movedItem] = logicCopy.splice(from, 1);
    if (!movedItem) return;
    logicCopy.splice(to, 0, movedItem);

    updateBlockLogic(blockIdx, logicCopy);
  };

  const duplicateLogic = (logicItemIdx: number) => {
    const logicCopy = structuredClone(blockLogic);
    const logicItem = logicCopy[logicItemIdx];
    if (!logicItem) return;
    const newLogicItem = duplicateLogicItem(logicItem);
    logicCopy.splice(logicItemIdx + 1, 0, newLogicItem);

    updateBlockLogic(blockIdx, logicCopy);
  };

  const [parent] = useAutoAnimate();

  useEffect(() => {
    if (blockLogic.length === 0 && blockLogicFallback) {
      updateBlockLogicFallback(blockIdx, undefined);
    }
  }, [blockLogic, blockIdx, blockLogicFallback, updateBlockLogicFallback]);

  // Expand the section whenever a rule is flagged invalid so the red outline is visible.
  // Keyed on invalidElements (a fresh array each validation pass) rather than only the derived
  // hasLogicError, so a re-save re-opens the section even if the user collapsed it while the
  // error still persisted (a plain edge-trigger on hasLogicError would not fire the second time).
  useEffect(() => {
    if (hasLogicError) {
      setIsOpen(true);
    }
  }, [hasLogicError, invalidElements]);

  return (
    <div className="mt-4">
      {blockLogic.length > 0 ? (
        <Collapsible.Root open={isOpen} onOpenChange={setIsOpen}>
          <Collapsible.CollapsibleTrigger
            className="flex cursor-pointer items-center gap-2 text-sm font-medium text-slate-900"
            aria-label={t("workspace.surveys.edit.conditional_logic")}>
            {isOpen ? (
              <ChevronDownIcon className="size-4 text-slate-500" />
            ) : (
              <ChevronRightIcon className="size-4 text-slate-500" />
            )}
            {t("workspace.surveys.edit.conditional_logic")}
            <SplitIcon className="size-4 rotate-90" />
            <span
              className={cn(
                "rounded px-1.5 py-0.5 text-xs font-normal",
                hasLogicError ? "bg-red-100 text-red-600" : "bg-slate-100 text-slate-500"
              )}>
              {blockLogic.length}
            </span>
          </Collapsible.CollapsibleTrigger>

          <Collapsible.CollapsibleContent>
            <div className="mt-2 flex flex-col gap-4" ref={parent}>
              {blockLogic.map((logicItem, logicItemIdx) => (
                <div
                  key={logicItem.id}
                  id={logicRuleDomId(logicItem.id)}
                  className={cn(
                    // scroll-mt clears the fixed tabs bar when a rule issue scrolls here
                    "flex w-full grow scroll-mt-16 items-start gap-2 rounded-lg border bg-slate-50 p-3",
                    invalidElements?.includes(logicItem.id) ? "border-red-400" : "border-slate-200"
                  )}>
                  {/* w-0 keeps the rule's own width (its widest row of selects) from widening the block card; LogicEditor scrolls horizontally inside the space left beside the menu. */}
                  <div className="w-0 min-w-0 flex-1">
                    <LogicEditor
                      localSurvey={transformedSurvey}
                      logicItem={logicItem}
                      updateBlockLogic={updateBlockLogic}
                      updateBlockLogicFallback={updateBlockLogicFallback}
                      block={block}
                      blockIdx={blockIdx}
                      logicIdx={logicItemIdx}
                      isLast={logicItemIdx === blockLogic.length - 1}
                    />
                  </div>

                  {/* Every rule has its own menu, beside the rule rather than over its first row, so a rule with a single condition can be moved, duplicated and removed too. */}
                  <DropdownMenu>
                    <DropdownMenuTrigger id={`logic-item-${logicItem.id}-dropdown`} asChild>
                      <Button
                        variant="secondary"
                        aria-label={t("workspace.surveys.edit.logic.rule_options")}
                        className="flex size-10 shrink-0 items-center justify-center rounded-md">
                        <EllipsisVerticalIcon className="size-4 text-slate-700 hover:text-slate-950" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent className="mt-10">
                      <DropdownMenuItem
                        onClick={() => {
                          duplicateLogic(logicItemIdx);
                        }}
                        icon={<CopyIcon className="size-4" />}>
                        {t("common.duplicate")}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        disabled={logicItemIdx === 0}
                        onClick={() => {
                          moveLogic(logicItemIdx, logicItemIdx - 1);
                        }}
                        icon={<ArrowUpIcon className="size-4" />}>
                        {t("common.move_up")}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        disabled={logicItemIdx === blockLogic.length - 1}
                        onClick={() => {
                          moveLogic(logicItemIdx, logicItemIdx + 1);
                        }}
                        icon={<ArrowDownIcon className="size-4" />}>
                        {t("common.move_down")}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onClick={() => {
                          handleRemoveLogic(logicItemIdx);
                        }}
                        icon={<TrashIcon className="size-4" />}>
                        {t("common.remove")}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              ))}
            </div>
          </Collapsible.CollapsibleContent>
        </Collapsible.Root>
      ) : (
        <Label className="flex gap-2">
          {t("workspace.surveys.edit.conditional_logic")}
          <SplitIcon className="size-4 rotate-90" />
        </Label>
      )}

      <div className="mt-2 flex items-center gap-x-2">
        <Button
          id="logicJumps"
          type="button"
          name="logicJumps"
          size="sm"
          variant="secondary"
          onClick={addLogic}>
          {t("workspace.surveys.edit.add_logic")}
          <PlusIcon />
        </Button>
      </div>
    </div>
  );
}
