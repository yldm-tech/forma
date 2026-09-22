import { getTranslate } from "@/lingodotdev/server";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";

// Two stacked cards, matching MembersView above TeamsView on the real page, so the boundary trades a
// frozen previous screen for the destination's own shape rather than for a layout shift.
const Loading = async () => {
  const t = await getTranslate();

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.teams")} />
      {["members", "teams"].map((section) => (
        <div key={section} className="w-full max-w-4xl rounded-xl border border-slate-200 bg-white shadow-xs">
          <div className="border-b border-slate-200 p-4">
            <div className="h-6 w-48 animate-pulse rounded-lg bg-slate-100">
              <span className="sr-only">{t("common.loading")}</span>
            </div>
            <div className="mt-3 h-4 w-80 animate-pulse rounded-lg bg-slate-100"></div>
          </div>
          <div className="space-y-4 p-4">
            {[...Array(3)].map((_, index) => (
              <div
                key={`${section}-row-${index.toString()}`}
                className="flex animate-pulse items-center gap-x-4">
                <div className="size-10 shrink-0 rounded-full bg-slate-200"></div>
                <div className="flex-1 space-y-2">
                  <div className="h-4 w-3/4 rounded-full bg-slate-200"></div>
                  <div className="h-3 w-1/2 rounded-full bg-slate-200"></div>
                </div>
              </div>
            ))}
          </div>
        </div>
      ))}
    </PageContentWrapper>
  );
};

export default Loading;
