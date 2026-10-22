import type { TSurvey } from "@forma/types/surveys/types";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Dialog, DialogBody, DialogTitle } from "../../../../components/ui/dialog";
import { LoadingSpinner } from "../../../../components/ui/loading-spinner";
import { errorMessage } from "../../../../lib/errors";
import { tagsQuery } from "../../../../lib/queries/tags";
import { analysisApi, invalidateSurveyAnalysis, toResponseTag } from "../../api";
import { SingleResponseCard } from "../response-card/single-response-card";

/** forma2's ResponseSampleModal: the response behind a summary sample, opened in place. */
export const ResponseSampleModal = ({
  responseId,
  onClose,
  survey,
  isReadOnly,
  locale,
}: {
  responseId: string | null;
  onClose: () => void;
  survey: TSurvey;
  isReadOnly: boolean;
  locale: string;
}) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const response = useQuery({
    queryKey: ["responses", responseId],
    queryFn: () => analysisApi.getResponse(responseId ?? ""),
    enabled: responseId !== null,
  });
  const tags = useQuery({ ...tagsQuery(survey.workspaceId), enabled: responseId !== null });

  const renderBody = () => {
    if (response.isPending || tags.isPending) {
      return (
        <div className="py-12">
          <LoadingSpinner />
        </div>
      );
    }
    if (response.isError || tags.isError) {
      return (
        <div className="py-12 text-center text-sm text-slate-600">
          {errorMessage(t, response.error ?? tags.error)}
        </div>
      );
    }
    return (
      <SingleResponseCard
        survey={survey}
        response={response.data}
        environmentTags={tags.data.map(toResponseTag)}
        isReadOnly={isReadOnly}
        locale={locale}
        onResponseChanged={() => {
          void response.refetch();
          void tags.refetch();
        }}
        onResponsesDeleted={() => {
          onClose();
          void invalidateSurveyAnalysis(queryClient, survey.id);
        }}
      />
    );
  };

  return (
    <Dialog open={responseId !== null} onOpenChange={(open) => !open && onClose()} width="wide">
      <DialogTitle className="sr-only">{t("common.response")}</DialogTitle>
      <DialogBody>{renderBody()}</DialogBody>
    </Dialog>
  );
};
