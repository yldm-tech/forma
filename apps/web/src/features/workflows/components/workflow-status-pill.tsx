import { useTranslation } from "react-i18next";
import { Badge } from "../../../components/ui/badge";
import type { TWorkflowStatus } from "../definition";
import { getWorkflowStatusBadge } from "../lib/display";

export const WorkflowStatusPill = ({
  status,
  size = "tiny",
}: Readonly<{ status: TWorkflowStatus; size?: "tiny" | "normal" | "large" }>) => {
  const { t } = useTranslation();
  const badge = getWorkflowStatusBadge(status, t);
  return <Badge text={badge.label} type={badge.type} size={size} />;
};
