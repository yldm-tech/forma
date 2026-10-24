import { useTranslation } from "react-i18next";
import { cn } from "../../lib/cn";

interface LegalFooterProps {
  imprintUrl?: string;
  privacyUrl?: string;
  termsUrl?: string;
  isFormaCloud: boolean;
  surveyUrl: string;
  isInFlow?: boolean;
  /** Text color for the links, resolved for AA contrast against the survey background. */
  linkColor?: string;
  /** When set (non-solid backgrounds), a surface rendered behind the links for legibility. */
  backdropColor?: string;
}

/** forma2's LegalFooter: the operator's imprint, privacy and terms links, and on Forma Cloud a link to report the survey. */
export const LegalFooter = ({
  imprintUrl,
  privacyUrl,
  termsUrl,
  isFormaCloud,
  surveyUrl,
  isInFlow = false,
  linkColor,
  backdropColor,
}: Readonly<LegalFooterProps>) => {
  const { t } = useTranslation();
  if (!imprintUrl && !privacyUrl && !termsUrl && !isFormaCloud) return null;

  return (
    <footer className={cn("z-1500 w-full", isInFlow ? "shrink-0 py-4" : "absolute bottom-0 h-10")}>
      <div className="mx-auto flex h-full max-w-2xl items-center justify-center p-2 text-center text-xs">
        <div
          className={cn(
            "flex items-center justify-center text-slate-500",
            backdropColor && "rounded-md px-3 py-1"
          )}
          style={{ color: linkColor, backgroundColor: backdropColor }}>
          {imprintUrl && (
            <a href={imprintUrl} target="_blank" rel="noreferrer" className="hover:underline">
              {t("common.imprint")}
            </a>
          )}
          {imprintUrl && privacyUrl && <span className="px-2">|</span>}
          {privacyUrl && (
            <a href={privacyUrl} target="_blank" rel="noreferrer" className="hover:underline">
              {t("common.privacy")}
            </a>
          )}
          {(imprintUrl || privacyUrl) && termsUrl && <span className="px-2">|</span>}
          {termsUrl && (
            <a href={termsUrl} target="_blank" rel="noreferrer" className="hover:underline">
              {t("common.terms_of_service")}
            </a>
          )}
          {(imprintUrl || privacyUrl || termsUrl) && isFormaCloud && <span className="px-2">|</span>}
          {isFormaCloud && (
            <a
              href={`https://forma.yldm.ai/s/clxbivtla014iye2vfrn436xd?surveyUrl=${surveyUrl}`}
              target="_blank"
              rel="noreferrer"
              className="hover:underline">
              {t("common.report_survey")}
            </a>
          )}
        </div>
      </div>
    </footer>
  );
};
