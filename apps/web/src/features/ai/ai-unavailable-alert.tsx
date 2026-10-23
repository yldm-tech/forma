import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Alert, AlertButton, AlertDescription, AlertTitle } from "../../components/ui/alert";
import { type AIUnavailableReason, getAIUnavailableMessage, offersEnableAI } from "./availability";

/**
 * forma2's AIUnavailableAlert: the one way to say AI smart tools are unavailable here. The title names the blocked capability, the reason's copy is shared, and switching AI on links to the organization settings.
 */
export const AIUnavailableAlert = ({
  title,
  reason,
  organizationId,
}: {
  title: string;
  reason?: AIUnavailableReason;
  organizationId?: string;
}) => {
  const { t } = useTranslation();

  return (
    <Alert variant="info" role="status">
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{getAIUnavailableMessage(reason, t)}</AlertDescription>
      {organizationId && offersEnableAI(reason) && (
        <AlertButton asChild>
          <Link to="/organizations/$organizationId/settings/general" params={{ organizationId }}>
            {t("common.ai_unavailable.enable_in_settings")}
          </Link>
        </AlertButton>
      )}
    </Alert>
  );
};
