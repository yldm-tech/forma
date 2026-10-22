import type { TResponse } from "@forma/types/responses";
import { Link } from "@tanstack/react-router";
import { TrashIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "../../../../components/ui/avatar";
import { Button } from "../../../../components/ui/button";
import { IdBadge } from "../../../../components/ui/id-badge";
import { TooltipRenderer } from "../../../../components/ui/tooltip";
import { timeSince } from "../../../../lib/time";
import { canDeleteResponse } from "../../lib/response-card";
import { getContactIdentifier } from "../../lib/responses";

/** forma2's SingleResponseCardHeader on the response pages: who answered, when, and the delete button. */
export const ResponseCardHeader = ({
  response,
  workspaceId,
  isReadOnly,
  setDeleteDialogOpen,
  locale,
}: {
  response: TResponse;
  workspaceId: string;
  isReadOnly: boolean;
  setDeleteDialogOpen: (open: boolean) => void;
  locale: string;
}) => {
  const { t } = useTranslation();
  const displayIdentifier = response.contact
    ? getContactIdentifier(response.contact, response.contactAttributes)
    : null;

  return (
    <div className="space-y-2 border-b border-slate-200 px-6 pt-4 pb-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center justify-center gap-x-2">
          {response.contact?.id ? (
            <Link
              className="flex items-center gap-x-2"
              to="/workspaces/$workspaceId/contacts/$contactId"
              params={{ workspaceId, contactId: response.contact.id }}>
              <PersonAvatar personId={response.contact.id} />
              <h3 className="ph-no-capture ml-4 pb-1 font-semibold text-slate-600 hover:underline">
                {displayIdentifier}
              </h3>
            </Link>
          ) : (
            <div className="flex items-center">
              <PersonAvatar personId="anonymous" />
              <h3 className="ml-4 pb-1 font-semibold text-slate-600">{t("common.anonymous")}</h3>
            </div>
          )}
          {response.contact?.userId && <IdBadge id={response.contact.userId} />}
        </div>

        <div className="flex items-center gap-x-2 text-sm">
          <time className="text-slate-500" dateTime={response.createdAt.toISOString()}>
            {timeSince(response.createdAt.toISOString(), locale)}
          </time>
          {!isReadOnly &&
            (canDeleteResponse(response) ? (
              <Button
                variant="ghost"
                size="icon"
                onClick={() => setDeleteDialogOpen(true)}
                aria-label={t("workspace.surveys.responses.delete_response")}>
                <TrashIcon className="size-4" />
              </Button>
            ) : (
              <TooltipRenderer tooltipContent={t("workspace.surveys.responses.this_response_is_in_progress")}>
                <Button
                  variant="ghost"
                  size="icon"
                  disabled
                  className="text-slate-400"
                  aria-label={t("workspace.surveys.responses.this_response_is_in_progress")}>
                  <TrashIcon className="size-4" />
                </Button>
              </TooltipRenderer>
            ))}
        </div>
      </div>
    </div>
  );
};
