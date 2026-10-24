import type { TResponseWithQuotas } from "@forma/types/responses";
import type { TSurvey } from "@forma/types/surveys/types";
import type { TTag } from "@forma/types/tags";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "../../../../components/ui/dialog";
import { SingleResponseCard } from "../response-card/single-response-card";

/** Whether an arrow key pressed with this element focused belongs to it: a field, an editable region, a combobox, a slider or a menu reads the arrows itself. */
export const isArrowKeyOwner = (target: EventTarget | null): boolean => {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  if (target.closest("input, textarea, select, [contenteditable='true']")) return true;
  return (
    target.closest(
      "[role='combobox'], [role='listbox'], [role='menu'], [role='menuitem'], [role='slider'], [role='textbox'], [role='tablist']"
    ) !== null
  );
};

/** forma2's ResponseCardModal: one response of the table, with previous and next. Next at the last loaded response loads the next page and moves on, ArrowLeft and ArrowRight step too (not while a field or a nested dialog has the keys), and the position is shown as "Response n of total". */
export const ResponseCardModal = ({
  responses,
  hasMore,
  fetchNextPage,
  totalCount,
  selectedResponseId,
  setSelectedResponseId,
  survey,
  environmentTags,
  onResponseChanged,
  onResponsesDeleted,
  isReadOnly,
  locale,
}: {
  responses: TResponseWithQuotas[];
  hasMore: boolean;
  /** Loads the next page and resolves to every loaded response, the new page included. */
  fetchNextPage: () => Promise<TResponseWithQuotas[]>;
  /** The filtered response count; the loaded count stands in until it is known. */
  totalCount: number | undefined;
  selectedResponseId: string | null;
  setSelectedResponseId: (id: string | null) => void;
  survey: TSurvey;
  environmentTags: TTag[];
  onResponseChanged: (responseId: string) => void;
  onResponsesDeleted: (responseIds: string[]) => void;
  isReadOnly: boolean;
  locale: string;
}) => {
  const { t } = useTranslation();
  const [isLoadingNext, setIsLoadingNext] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);
  // The response on screen as of the last render: a page that arrives after the modal closed or moved on does not move it.
  const shownIdRef = useRef(selectedResponseId);
  useEffect(() => {
    shownIdRef.current = selectedResponseId;
  });
  const currentIndex =
    selectedResponseId === null ? -1 : responses.findIndex((response) => response.id === selectedResponseId);
  // A deleted response leaves the list before the selection is cleared; an index past the end shows nothing rather than a card for no response.
  const response = currentIndex >= 0 ? responses[currentIndex] : undefined;
  const isLast = currentIndex === responses.length - 1;
  const canGoBack = currentIndex > 0;
  const canGoNext = !isLast || hasMore;
  const total = Math.max(totalCount ?? 0, responses.length);

  const goBack = () => {
    if (canGoBack && !isLoadingNext) setSelectedResponseId(responses[currentIndex - 1]?.id ?? null);
  };

  const goNext = async () => {
    if (!response || isLoadingNext) return;
    if (!isLast) {
      setSelectedResponseId(responses[currentIndex + 1]?.id ?? null);
      return;
    }
    if (!hasMore) return;
    setIsLoadingNext(true);
    try {
      const loaded = await fetchNextPage();
      const index = loaded.findIndex((candidate) => candidate.id === response.id);
      const next = index >= 0 ? loaded[index + 1] : undefined;
      if (next && shownIdRef.current === response.id) setSelectedResponseId(next.id);
    } finally {
      setIsLoadingNext(false);
    }
  };

  const isOpen = response !== undefined;

  // Subscribed again every render, so the handler always steps from the current response.
  useEffect(() => {
    if (!isOpen) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey || event.defaultPrevented) return;
      if (isArrowKeyOwner(event.target)) return;
      // A dialog opened from the card (delete, tags) is a later top-layer dialog: the keys are its own.
      const openDialogs = document.querySelectorAll("dialog[open]");
      const topDialog = openDialogs[openDialogs.length - 1];
      if (topDialog && dialogRef.current && !topDialog.contains(dialogRef.current)) return;
      event.preventDefault();
      if (event.key === "ArrowLeft") goBack();
      else void goNext();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  });

  if (!response) return null;

  const position = t("workspace.surveys.responses.response_position", {
    position: currentIndex + 1,
    total,
  });

  return (
    <Dialog open onOpenChange={(open) => !open && setSelectedResponseId(null)} width="wide">
      <DialogTitle className="sr-only">{t("common.response")}</DialogTitle>
      <DialogDescription className="sr-only">{position}</DialogDescription>
      <DialogBody>
        <div ref={dialogRef} className="my-3">
          <SingleResponseCard
            survey={survey}
            response={response}
            environmentTags={environmentTags}
            isReadOnly={isReadOnly}
            onResponseChanged={onResponseChanged}
            onResponsesDeleted={(ids) => {
              setSelectedResponseId(null);
              onResponsesDeleted(ids);
            }}
            locale={locale}
          />
        </div>
      </DialogBody>
      <DialogFooter className="sm:items-center">
        <p className="mr-auto text-sm text-slate-600" aria-hidden="true">
          {position}
        </p>
        <Button
          onClick={goBack}
          disabled={!canGoBack || isLoadingNext}
          variant="outline"
          size="icon"
          aria-label={t("common.back")}
          aria-keyshortcuts="ArrowLeft">
          <ChevronLeft />
        </Button>
        <Button
          onClick={() => void goNext()}
          disabled={!canGoNext}
          loading={isLoadingNext}
          variant="outline"
          size="icon"
          aria-label={t("common.next")}
          aria-keyshortcuts="ArrowRight">
          <ChevronRight />
        </Button>
      </DialogFooter>
    </Dialog>
  );
};
