import type { ReactNode } from "react";

/** The empty result of the summary and the response table: what is missing, and the one action that moves on from it (share the survey when nothing came in yet, clear the filters when nothing matches them). */
export const AnalysisEmptyState = ({ title, action }: { title: string; action?: ReactNode }) => (
  <div className="flex flex-col items-center justify-center gap-3 bg-white px-6 py-12 text-center">
    <p className="text-sm font-medium text-slate-700">{title}</p>
    {action}
  </div>
);
