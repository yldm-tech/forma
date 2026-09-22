import { getTranslate } from "@/lingodotdev/server";
import { ContactsSecondaryNavigation } from "@/modules/contacts/components/contacts-secondary-navigation";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";

// Mirrors ContactsPageLayout: the same wrapper, header and tab bar the resolved page renders, so the
// only thing that arrives late is the table body. The tab bar is rendered in its `loading` state
// because the route params are not available to a loading boundary.
const Loading = async () => {
  const t = await getTranslate();

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.contacts")}>
        <ContactsSecondaryNavigation activeId="contacts" loading />
      </PageHeader>
      <div className="mt-4 rounded-xl border border-slate-200 bg-white shadow-xs">
        <div className="grid h-12 grid-cols-6 content-center border-b border-slate-200 px-6 text-left text-sm font-semibold text-slate-900">
          <div className="col-span-3">{t("common.contacts")}</div>
          <div className="col-span-3 hidden sm:block">{t("common.attributes")}</div>
        </div>
        {[...Array(5)].map((_, index) => (
          <div
            key={`contact-loading-${index.toString()}`}
            className="m-2 grid h-14 animate-pulse grid-cols-6 content-center gap-x-4 rounded-lg px-4">
            <div className="col-span-3 h-4 rounded-full bg-slate-200"></div>
            <div className="col-span-3 hidden h-4 rounded-full bg-slate-200 sm:block"></div>
          </div>
        ))}
      </div>
    </PageContentWrapper>
  );
};

export default Loading;
