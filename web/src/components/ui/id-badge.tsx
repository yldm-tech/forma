import { Check, Copy } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { copyText } from "../../lib/clipboard";

/** forma2's IdBadge (column variant): an identifier with a copy button. */
export const IdBadge = ({ id, label }: { id: string; label: string }) => {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 5000);
    return () => clearTimeout(timer);
  }, [copied]);

  return (
    <div className="flex flex-col items-start gap-1">
      <span className="text-xs text-slate-700">{label}</span>
      <button
        type="button"
        aria-label={`${t("common.copy")} ${label} ${id}`}
        title={copied ? t("common.copied") : t("common.copy")}
        onClick={async () => setCopied(await copyText(id, t))}
        className="inline-flex cursor-pointer items-center gap-2 rounded-md border border-slate-200 px-2 py-1 text-xs text-slate-500 hover:border-transparent hover:bg-slate-900/80 hover:text-slate-50 focus-visible:ring-2 focus-visible:ring-slate-400 focus-visible:ring-offset-2 focus-visible:outline-hidden">
        <span>{id}</span>
        {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
      </button>
    </div>
  );
};
