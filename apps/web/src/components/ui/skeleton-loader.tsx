import { cn } from "../../lib/cn";

const Skeleton = ({ className }: { className?: string }) => (
  <div className={cn("animate-pulse", className)} />
);

const TableCells = () => (
  <>
    <Skeleton className="size-4 rounded-xl bg-slate-400" />
    <Skeleton className="h-4 w-24 rounded-xl bg-slate-200" />
    <Skeleton className="h-4 w-32 rounded-xl bg-slate-200" />
    <Skeleton className="h-4 w-40 rounded-xl bg-slate-200" />
    <Skeleton className="h-4 w-40 rounded-xl bg-slate-200" />
    <Skeleton className="h-4 w-32 rounded-xl bg-slate-200" />
  </>
);

/** forma2's SkeletonLoader: the placeholder of a summary card, the response table or one response. */
export const SkeletonLoader = ({ type }: { type: "response" | "responseTable" | "summary" }) => {
  if (type === "summary") {
    return (
      <div
        className="rounded-xl border border-slate-200 bg-white shadow-xs"
        data-testid="skeleton-loader-summary">
        <div className="animate-pulse space-y-4 rounded-xl bg-white p-6">
          <div className="flex gap-4">
            <div className="h-6 w-24 rounded-full bg-slate-200" />
            <div className="h-6 w-24 rounded-full bg-slate-200" />
          </div>
          <div className="h-12 w-full rounded-full bg-slate-200" />
          <div className="h-12 w-full rounded-full bg-slate-200" />
          <div className="h-12 w-full rounded-full bg-slate-200" />
        </div>
      </div>
    );
  }

  if (type === "responseTable") {
    return (
      <div className="animate-pulse space-y-4" data-testid="skeleton-loader-response-table">
        <div className="flex items-center justify-between">
          <Skeleton className="h-8 w-48 rounded-md bg-slate-300" />
          <div className="flex gap-2">
            <Skeleton className="size-8 rounded-md bg-slate-300" />
            <Skeleton className="size-8 rounded-md bg-slate-300" />
          </div>
        </div>
        <div className="overflow-hidden rounded-xl border border-slate-200">
          <div className="flex h-12 items-center gap-4 border-b border-slate-200 bg-slate-100 px-4">
            <TableCells />
          </div>
          {Array.from({ length: 10 }, (_, i) => (
            <div
              key={i}
              className="flex h-12 items-center gap-4 border-b border-slate-100 px-4 last:border-b-0">
              <TableCells />
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="group space-y-4 rounded-lg bg-white p-6" data-testid="skeleton-loader-response">
      <div className="flex items-center gap-x-4">
        <Skeleton className="size-12 shrink-0 rounded-full bg-slate-200" />
        <Skeleton className="h-6 w-full rounded-full bg-slate-200" />
      </div>
      <div className="space-y-4">
        <Skeleton className="h-12 w-full rounded-full bg-slate-200" />
        <Skeleton className="h-12 w-full rounded-full bg-slate-200" />
        <Skeleton className="h-12 w-full rounded-full bg-slate-200" />
      </div>
    </div>
  );
};
