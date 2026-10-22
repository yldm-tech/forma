import { ArrowUpRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Alert, AlertButton, AlertTitle } from "../../../../components/ui/alert";

/** forma2's DocumentationLinks: one outbound link per row, each opening the documentation in a new tab. */
export const DocumentationLinks = ({ links }: { links: { title: string; href: string }[] }) => {
  const { t } = useTranslation();
  return (
    <div className="flex w-full flex-col gap-y-2">
      {links.map((link) => (
        <Alert key={link.title} size="small" role="status">
          <ArrowUpRight />
          <AlertTitle>{link.title}</AlertTitle>
          <AlertButton asChild>
            <a
              href={link.href}
              target="_blank"
              rel="noopener noreferrer"
              className="text-slate-900 hover:underline">
              {t("common.learn_more")}
            </a>
          </AlertButton>
        </Alert>
      ))}
    </div>
  );
};
