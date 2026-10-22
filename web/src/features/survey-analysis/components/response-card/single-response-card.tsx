import type { TResponseWithQuotas } from "@forma/types/responses";
import type { TSurvey } from "@forma/types/surveys/types";
import type { TTag } from "@forma/types/tags";
import { type ReactNode, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { DeleteDialog } from "../../../../components/ui/delete-dialog";
import { errorMessage } from "../../../../lib/errors";
import { toast } from "../../../../lib/toast";
import { getElementsFromBlocks } from "../../../survey-editor/lib/client-utils";
import { analysisApi } from "../../api";
import { canDeleteResponse, getSkippedElementGroups } from "../../lib/response-card";
import { DecrementQuotasCheckbox } from "../response-values/response-values";
import { ResponseCardBody } from "./response-card-body";
import { ResponseCardHeader } from "./response-card-header";
import { ResponseTags } from "./response-tags";

export interface ResponseCardHeaderRenderProps {
  onDeleteClick: () => void;
  canResponseBeDeleted: boolean;
}

/**
 * forma2's SingleResponseCard: one response with its answers, skips, fields, tags and metadata, deletable by a member who may write. Deleting asks whether to give the response's quota places back, checked when it has any.
 */
export const SingleResponseCard = ({
  survey,
  response,
  environmentTags,
  onResponseChanged,
  onResponsesDeleted,
  isReadOnly,
  locale,
  renderHeader,
}: {
  survey: TSurvey;
  response: TResponseWithQuotas;
  environmentTags: TTag[];
  /** Called after the response's tags changed, to read it again. */
  onResponseChanged?: (responseId: string) => void;
  /** Called after the response was deleted. */
  onResponsesDeleted?: (responseIds: string[]) => void;
  isReadOnly: boolean;
  locale: string;
  renderHeader?: (props: ResponseCardHeaderRenderProps) => ReactNode;
}) => {
  const { t } = useTranslation();
  const hasQuotas = (response.quotas?.length ?? 0) > 0;
  const [decrementQuotas, setDecrementQuotas] = useState(hasQuotas);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  const skippedQuestions = useMemo(
    () => getSkippedElementGroups(getElementsFromBlocks(survey.blocks), response),
    [survey.blocks, response]
  );

  const handleDeleteResponse = async () => {
    setIsDeleting(true);
    try {
      await analysisApi.deleteResponse(response.id, decrementQuotas);
      onResponsesDeleted?.([response.id]);
      toast.success(t("workspace.surveys.responses.response_deleted_successfully"));
      setDeleteDialogOpen(false);
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setIsDeleting(false);
    }
  };

  const canResponseBeDeleted = canDeleteResponse(response);

  return (
    <div className="group relative">
      <div className="relative z-20 rounded-xl border border-slate-200 bg-white shadow-xs transition-all">
        {renderHeader ? (
          renderHeader({ onDeleteClick: () => setDeleteDialogOpen(true), canResponseBeDeleted })
        ) : (
          <ResponseCardHeader
            response={response}
            workspaceId={survey.workspaceId}
            isReadOnly={isReadOnly}
            setDeleteDialogOpen={setDeleteDialogOpen}
            locale={locale}
          />
        )}
        <ResponseCardBody
          survey={survey}
          response={response}
          skippedQuestions={skippedQuestions}
          locale={locale}
        />
        <ResponseTags
          key={response.id}
          workspaceId={survey.workspaceId}
          responseId={response.id}
          tags={response.tags.map((tag) => ({ tagId: tag.id, tagName: tag.name }))}
          environmentTags={environmentTags}
          onTagsChanged={() => onResponseChanged?.(response.id)}
          isReadOnly={isReadOnly}
          response={response}
          locale={locale}
        />
        <DeleteDialog
          open={deleteDialogOpen}
          setOpen={setDeleteDialogOpen}
          deleteWhat={t("common.response")}
          onDelete={() => void handleDeleteResponse()}
          isDeleting={isDeleting}
          text={t("workspace.surveys.responses.delete_response_confirmation")}>
          {hasQuotas && (
            <DecrementQuotasCheckbox
              title={t("workspace.surveys.responses.delete_response_quotas")}
              checked={decrementQuotas}
              onCheckedChange={setDecrementQuotas}
            />
          )}
        </DeleteDialog>
      </div>
    </div>
  );
};
