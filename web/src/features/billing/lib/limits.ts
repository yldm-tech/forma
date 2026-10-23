import type { BillingSummary } from "../../../lib/queries/billing";

/** The largest upload a survey's file question may allow when nothing caps it: 1 GB. */
export const UNCAPPED_UPLOAD_SIZE_MB = 1024;

/** forma2's file-upload cap of the survey editor: on Forma Cloud an organization with more than one workspace (a paid plan) may allow 1 GB per file and any other 10 MB; elsewhere nothing is capped. Until the billing summary has loaded, or when it cannot be read, the instance is treated as uncapped. */
export const maxUploadSizeMB = (summary: BillingSummary | undefined): number | null => {
  if (!summary?.isFormaCloud) return null;
  const { workspaces } = summary.limits;
  return workspaces === null || workspaces > 1 ? UNCAPPED_UPLOAD_SIZE_MB : 10;
};
