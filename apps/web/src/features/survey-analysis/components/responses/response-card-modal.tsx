import type { TResponseWithQuotas } from "@forma/types/responses";
import type { TSurvey } from "@forma/types/surveys/types";
import type { TTag } from "@forma/types/tags";
import { ChevronLeft, ChevronRight } from "lucide-react";
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

/** forma2's ResponseCardModal: one response of the table, with previous and next. */
export const ResponseCardModal = ({
  responses,
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
  const currentIndex =
    selectedResponseId === null ? -1 : responses.findIndex((response) => response.id === selectedResponseId);
  // A deleted response leaves the list before the selection is cleared; an index past the end shows nothing rather than a card for no response.
  const response = currentIndex >= 0 ? responses[currentIndex] : undefined;
  if (!response) return null;

  return (
    <Dialog open onOpenChange={(open) => !open && setSelectedResponseId(null)} width="wide">
      <DialogTitle className="sr-only">{t("common.response")}</DialogTitle>
      <DialogDescription className="sr-only">{`${currentIndex + 1} / ${responses.length}`}</DialogDescription>
      <DialogBody>
        <div className="my-3">
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
      <DialogFooter>
        <Button
          onClick={() => setSelectedResponseId(responses[currentIndex - 1]?.id ?? null)}
          disabled={currentIndex === 0}
          variant="outline"
          size="icon"
          aria-label={t("common.back")}>
          <ChevronLeft />
        </Button>
        <Button
          onClick={() => setSelectedResponseId(responses[currentIndex + 1]?.id ?? null)}
          disabled={currentIndex === responses.length - 1}
          variant="outline"
          size="icon"
          aria-label={t("common.next")}>
          <ChevronRight />
        </Button>
      </DialogFooter>
    </Dialog>
  );
};
