import type { TFunction } from "i18next";
import { type ReactNode, type RefObject, memo, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";
import { getElementTypes } from "../../survey-editor/lib/elements";
import { type AiDraftQuestion, type AiDraftState, groupAiDraftByBlock } from "./draft-reducer";

/** How close to the bottom still counts as following along, in px. */
const PIN_THRESHOLD_PX = 32;
const PENDING_ROW_COUNT = 3;
const CHOICE_ELEMENT_TYPES = new Set(["multipleChoiceSingle", "multipleChoiceMulti", "ranking", "matrix"]);

const Placeholder = ({ className }: { className: string }) => (
  <div aria-hidden="true" className={cn("animate-pulse rounded-md bg-slate-200", className)} />
);

/** Memoised, and the reducer keeps an unchanged question's object, so a snapshot re-renders only the rows that moved. */
const AiDraftRow = memo(
  ({
    question,
    icon,
    typeName,
    t,
  }: {
    question: AiDraftQuestion;
    icon?: ReactNode;
    typeName?: string;
    t: TFunction;
  }) => {
    const optionCount =
      question.type && CHOICE_ELEMENT_TYPES.has(question.type) && question.choiceCount
        ? t("workspace.surveys.ai_create.option_count", { count: question.choiceCount })
        : undefined;
    return (
      <li className="flex items-start gap-3 px-4 py-3">
        <span className="mt-0.5 flex shrink-0 items-center text-slate-600">{icon}</span>
        <div className="min-w-0 flex-1">
          {question.headline ? (
            // Truncated, never wrapped: a headline growing character by character keeps the row's height.
            <h3 className="truncate text-sm font-semibold text-slate-800">{question.headline}</h3>
          ) : (
            <Placeholder className="my-1 h-3.5 w-48" />
          )}
          <p className="mt-1 truncate text-xs text-slate-500">
            {typeName ?? " "}
            {optionCount ? ` · ${optionCount}` : ""}
          </p>
        </div>
      </li>
    );
  }
);
AiDraftRow.displayName = "AiDraftRow";

/**
 * forma2's AiDraftPreview: the draft as the model writes it, grouped into its blocks, with the glyphs and labels the editor uses. The list follows the newest question unless the user scrolled away from the bottom.
 */
export const AiDraftPreview = ({
  draft,
  isGenerating,
  className,
  scrollContainerRef,
}: {
  draft: AiDraftState;
  isGenerating: boolean;
  className?: string;
  /** The scroll container, so a finished generation can land focus where scrolling works. */
  scrollContainerRef?: RefObject<HTMLElement | null>;
}) => {
  const { t } = useTranslation();
  const elementTypes = useMemo(
    () => new Map(getElementTypes(t).map((type) => [type.id as string, type])),
    [t]
  );
  const blocks = useMemo(() => groupAiDraftByBlock(draft.questions), [draft.questions]);
  const showsBlockNames = blocks.length > 1;
  const scrollRef = useRef<HTMLElement | null>(null);
  const isPinnedRef = useRef(true);

  useEffect(() => {
    const element = scrollRef.current;
    if (!element || !isPinnedRef.current) return;
    const prefersReducedMotion = globalThis.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    element.scrollTo?.({ top: element.scrollHeight, behavior: prefersReducedMotion ? "auto" : "smooth" });
  }, [draft.questions.length]);

  const handleScroll = () => {
    const element = scrollRef.current;
    if (!element) return;
    isPinnedRef.current = element.scrollHeight - element.scrollTop - element.clientHeight < PIN_THRESHOLD_PX;
  };

  return (
    <div
      className={cn(
        "relative flex min-h-0 flex-col overflow-hidden rounded-lg border border-slate-200 bg-white",
        className
      )}>
      <div className="shrink-0 border-b border-slate-100 px-4 py-3">
        {draft.name ? (
          <p className="truncate text-sm font-semibold text-slate-800">{draft.name}</p>
        ) : (
          <Placeholder className="my-0.5 h-4 w-56" />
        )}
      </div>
      {/* The scroll container itself takes focus, so a keyboard user can scroll the draft (WCAG 2.1.1); a named section is a region on its own. */}
      <section
        ref={(node) => {
          scrollRef.current = node;
          if (scrollContainerRef) scrollContainerRef.current = node;
        }}
        onScroll={handleScroll}
        tabIndex={0}
        aria-label={t("workspace.surveys.ai_create.draft_survey")}
        aria-busy={isGenerating}
        className="focus-visible:ring-ring min-h-0 flex-1 overflow-y-auto focus-visible:ring-1 focus-visible:outline-hidden">
        {blocks.map((block) => (
          <section key={block.key} className="-mt-px first:mt-0">
            {showsBlockNames && (
              <h4 className="sticky top-0 z-10 border-y border-slate-200 bg-slate-50 px-4 py-1.5 text-xs font-medium text-slate-500">
                {block.name ?? <Placeholder className="my-0.5 inline-block h-3 w-24 align-middle" />}
              </h4>
            )}
            <ul className="divide-y divide-slate-100">
              {block.questions.map((question) => {
                const type = question.type ? elementTypes.get(question.type) : undefined;
                return (
                  <AiDraftRow
                    key={question.key}
                    question={question}
                    icon={type ? <type.icon className="size-4" /> : undefined}
                    typeName={type?.label}
                    t={t}
                  />
                );
              })}
            </ul>
          </section>
        ))}
        {isGenerating && (
          <ul className="divide-y divide-slate-100">
            {Array.from({ length: PENDING_ROW_COUNT }, (_, index) => (
              <li key={`pending-${index}`} className="flex items-start gap-3 px-4 py-3">
                <Placeholder className="mt-0.5 size-4 shrink-0" />
                <div className="flex-1 space-y-2">
                  <Placeholder className="h-3.5 w-44" />
                  <Placeholder className="h-2.5 w-16" />
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
};
