import { getTranslate } from "@/lingodotdev/server";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";

// The page titles itself with the contact's own identifier, which a loading boundary cannot know, so
// the title is a bar rather than a wrong label. The two-column split below matches the attributes and
// activity sections.
const Loading = async () => {
  const t = await getTranslate();

  return (
    <PageContentWrapper>
      <div className="h-8 w-28 animate-pulse rounded-lg bg-slate-200">
        <span className="sr-only">{t("common.loading")}</span>
      </div>
      <PageHeader pageTitle={<span className="block h-8 w-64 animate-pulse rounded-lg bg-slate-200" />} />
      <section className="pt-6 pb-24">
        <div className="grid grid-cols-4 gap-x-8">
          <div className="col-span-1 space-y-4">
            {[...Array(6)].map((_, index) => (
              <div key={`attribute-row-${index.toString()}`} className="space-y-2">
                <div className="h-3 w-24 animate-pulse rounded-full bg-slate-200"></div>
                <div className="h-4 w-40 animate-pulse rounded-full bg-slate-200"></div>
              </div>
            ))}
          </div>
          <div className="col-span-3 space-y-4">
            {[...Array(4)].map((_, index) => (
              <div
                key={`activity-row-${index.toString()}`}
                className="h-20 animate-pulse rounded-xl border border-slate-200 bg-slate-100"></div>
            ))}
          </div>
        </div>
      </section>
    </PageContentWrapper>
  );
};

export default Loading;
