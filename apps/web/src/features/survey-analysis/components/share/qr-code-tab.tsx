import { Download, LoaderCircle } from "lucide-react";
import QRCodeStyling from "qr-code-styling";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "../../../../components/ui/alert";
import { Button } from "../../../../components/ui/button";
import { toast } from "../../../../lib/toast";
import { getQRCodeOptions } from "../../lib/qr-code-options";

/** forma2's QRCodeTab: the link as a QR code, downloadable as a 500 px PNG. */
export const QRCodeTab = ({ surveyUrl }: { surveyUrl: string }) => {
  const { t } = useTranslation();
  const qrCodeRef = useRef<HTMLDivElement>(null);
  const [hasError, setHasError] = useState(false);
  const [isDownloading, setIsDownloading] = useState(false);

  useEffect(() => {
    if (!surveyUrl) return;
    try {
      const qrCode = new QRCodeStyling(getQRCodeOptions(184, 184));
      qrCode.update({ data: surveyUrl });
      if (qrCodeRef.current) {
        qrCodeRef.current.innerHTML = "";
        qrCode.append(qrCodeRef.current);
      }
    } catch {
      setHasError(true);
    }
  }, [surveyUrl]);

  const downloadQRCode = async () => {
    setIsDownloading(true);
    try {
      const downloadInstance = new QRCodeStyling(getQRCodeOptions(500, 500));
      downloadInstance.update({ data: surveyUrl });
      await downloadInstance.download({ name: "survey-qr-code", extension: "png" });
      toast.success(t("workspace.surveys.summary.qr_code_download_with_start_soon"));
    } catch {
      toast.error(t("workspace.surveys.summary.qr_code_download_failed"));
    } finally {
      setIsDownloading(false);
    }
  };

  if (hasError) {
    return (
      <Alert variant="error">
        <AlertTitle>{t("common.something_went_wrong")}</AlertTitle>
        <AlertDescription>{t("workspace.surveys.summary.qr_code_generation_failed")}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="flex flex-col items-start justify-center gap-4">
      <div className="flex size-[184px] items-center justify-center overflow-hidden rounded-lg border bg-white">
        <div ref={qrCodeRef} className="h-full w-full" />
      </div>
      <Button
        onClick={() => void downloadQRCode()}
        data-testid="download-qr-code-button"
        disabled={!surveyUrl || isDownloading}
        className="flex items-center gap-2">
        {isDownloading
          ? t("workspace.surveys.summary.downloading_qr_code")
          : t("workspace.surveys.summary.download_qr_code")}
        {isDownloading ? <LoaderCircle className="size-4 animate-spin" /> : <Download className="size-4" />}
      </Button>
    </div>
  );
};
