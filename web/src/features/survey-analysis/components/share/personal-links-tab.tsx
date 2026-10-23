import { useQuery } from "@tanstack/react-query";
import { DownloadIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { DatePicker } from "../../../../components/ui/date-picker";
import { Label } from "../../../../components/ui/label";
import { Select } from "../../../../components/ui/select";
import { generatePersonalLinks, segmentsQuery } from "../../../../lib/queries/contacts";
import { toast } from "../../../../lib/toast";
import { personalLinkErrorMessage } from "../../../contacts/personal-link-errors";
import { DocumentationLinks } from "./documentation-links";

const TOAST_ID = "generating-links";
const DAY_MS = 1000 * 60 * 60 * 24;

/** A personal link has to expire in the future, so the calendar starts at tomorrow. */
const getTomorrow = (now: Date = new Date()): Date => {
  const tomorrow = new Date(now);
  tomorrow.setDate(tomorrow.getDate() + 1);
  tomorrow.setHours(0, 0, 0, 0);
  return tomorrow;
};

/** The days until the chosen day. The picker gives local midnight of that day while the token's lifetime runs from now, so a partial day rounds up: rounding down would expire the links a day early. */
export const expirationDaysUntil = (expiryDate: Date, now: Date = new Date()): number =>
  Math.max(1, Math.ceil((expiryDate.getTime() - now.getTime()) / DAY_MS));

/**
 * forma2's PersonalLinksTab: pick one of the workspace's public segments and, optionally, the day the links expire, and download a CSV with a personal link for every contact of the segment.
 */
export const PersonalLinksTab = ({ surveyId, workspaceId }: { surveyId: string; workspaceId: string }) => {
  const { t, i18n } = useTranslation();
  const segments = useQuery(segmentsQuery(workspaceId));
  const [selectedSegment, setSelectedSegment] = useState("");
  const [expiryDate, setExpiryDate] = useState<Date | null>(null);
  const [isGenerating, setIsGenerating] = useState(false);
  const publicSegments = (segments.data ?? []).filter((segment) => !segment.isPrivate);

  const handleGenerateLinks = async () => {
    if (!selectedSegment || isGenerating) return;
    setIsGenerating(true);
    toast.loading(t("workspace.surveys.share.personal_links.generating_links_toast"), { id: TOAST_ID });
    try {
      const file = await generatePersonalLinks(
        surveyId,
        selectedSegment,
        expiryDate ? expirationDaysUntil(expiryDate) : undefined
      );
      const fileName = file.fileName || "personal-links.csv";
      const url = URL.createObjectURL(new File([file.csvContent], fileName, { type: "text/csv" }));
      const link = document.createElement("a");
      link.href = url;
      link.download = fileName;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
      toast.success(t("workspace.surveys.share.personal_links.links_generated_success_toast"), {
        id: TOAST_ID,
      });
    } catch (error) {
      toast.error(personalLinkErrorMessage(t, error), { id: TOAST_ID });
    } finally {
      setIsGenerating(false);
    }
  };

  const noSegments = publicSegments.length === 0;

  return (
    <div className="flex h-full flex-col justify-between gap-y-4">
      <div className="flex grow flex-col gap-6">
        <div className="space-y-2">
          <Label htmlFor="personal-links-segment">{t("common.recipients")}</Label>
          <Select
            id="personal-links-segment"
            value={selectedSegment}
            disabled={noSegments}
            onChange={(e) => setSelectedSegment(e.target.value)}
            className="bg-white">
            <option value="" disabled>
              {noSegments
                ? t("workspace.surveys.share.personal_links.no_segments_available")
                : t("workspace.surveys.share.personal_links.select_segment")}
            </option>
            {publicSegments.map((segment) => (
              <option key={segment.id} value={segment.id}>
                {segment.title}
              </option>
            ))}
          </Select>
          <p className="text-sm text-slate-500">
            {t("workspace.surveys.share.personal_links.create_and_manage_segments")}
          </p>
        </div>
        <div className="space-y-2">
          <Label>{t("workspace.surveys.share.personal_links.expiry_date_optional")}</Label>
          <DatePicker
            value={expiryDate}
            locale={i18n.resolvedLanguage ?? i18n.language ?? "en-US"}
            minDate={getTomorrow()}
            onChange={setExpiryDate}
            onClear={() => setExpiryDate(null)}
          />
          <p className="text-sm text-slate-500">
            {t("workspace.surveys.share.personal_links.expiry_date_description")}
          </p>
        </div>
        <Button
          onClick={() => void handleGenerateLinks()}
          disabled={!selectedSegment || isGenerating || noSegments}
          loading={isGenerating}
          className="w-fit">
          <DownloadIcon className="mr-2 size-4" />
          {isGenerating
            ? t("workspace.surveys.share.personal_links.generating_links")
            : t("workspace.surveys.share.personal_links.generate_and_download_links")}
        </Button>
      </div>
      <DocumentationLinks
        links={[
          {
            title: t("workspace.surveys.share.personal_links.work_with_segments"),
            href: "https://forma.yldm.ai/docs/surveys/website-app-surveys/advanced-targeting#segment-configuration",
          },
        ]}
      />
    </div>
  );
};
