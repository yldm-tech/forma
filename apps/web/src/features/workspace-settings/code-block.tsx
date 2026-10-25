import { CopyIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { copyText } from "../../lib/clipboard";

/** A code sample with a button that copies it. */
export const CodeBlock = ({ children }: { children: string }) => {
  const { t } = useTranslation();
  return (
    <div className="relative">
      <Button
        variant="ghost"
        size="icon"
        className="absolute top-2 right-2"
        aria-label={t("common.copy")}
        onClick={() => void copyText(children, t)}>
        <CopyIcon className="size-4" />
      </Button>
      <pre className="overflow-x-auto rounded-md border border-slate-200 bg-white p-4 pr-12 font-mono text-xs text-slate-800">
        <code>{children}</code>
      </pre>
    </div>
  );
};
