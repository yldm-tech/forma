// The placeholders the workflows pages show while their first page loads, forma2's modules/workflows/loading.tsx.

const Bar = ({ className }: { className: string }) => (
  <div className={`animate-pulse rounded-full bg-slate-200 ${className}`} />
);

export const WorkflowsListBodyLoading = () => (
  <div className="space-y-3" aria-hidden="true">
    {[1, 2, 3].map((row) => (
      <div
        key={row}
        className="grid h-16 w-full grid-cols-7 place-items-center gap-3 rounded-xl border border-slate-200 bg-white p-4 pr-8 shadow-xs">
        <Bar className="col-span-2 h-4 w-40 justify-self-start bg-slate-300" />
        {[1, 2, 3, 4, 5].map((cell) => (
          <Bar key={cell} className="h-4 w-16" />
        ))}
      </div>
    ))}
  </div>
);

export const RunsTableSkeleton = () => (
  <div className="w-full space-y-px overflow-hidden rounded-xl border border-slate-200" aria-hidden="true">
    {[1, 2, 3, 4, 5].map((row) => (
      <div key={row} className="flex h-12 items-center gap-6 bg-white px-4">
        <Bar className="h-4 w-1/3 bg-slate-300" />
        <Bar className="h-4 w-20" />
        <Bar className="h-4 w-24" />
        <Bar className="h-4 w-24" />
      </div>
    ))}
  </div>
);

export const WorkflowBuilderBodyLoading = () => (
  <div className="flex min-h-[480px] flex-1 gap-4" aria-hidden="true">
    <div className="flex flex-1 animate-pulse items-center justify-center rounded-lg border border-slate-200 bg-slate-50">
      <div className="h-14 w-64 rounded-lg bg-slate-200" />
    </div>
  </div>
);
