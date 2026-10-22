import type { ReactNode } from "react";
import { Small } from "../../../../components/ui/typography";

/** forma2's TabContainer: a share tab's title and description above its content. */
export const TabContainer = ({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) => (
  <div className="flex h-full grow flex-col items-start gap-y-4">
    <div className="pb-2">
      <h3 className="text-lg font-semibold text-slate-800">{title}</h3>
      <Small color="muted" className="mt-1">
        {description}
      </Small>
    </div>
    <div className="h-full w-full space-y-4 overflow-y-auto">{children}</div>
  </div>
);
