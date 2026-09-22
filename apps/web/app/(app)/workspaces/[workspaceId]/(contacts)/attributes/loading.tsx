import { getTranslate } from "@/lingodotdev/server";
import { ContactsSecondaryNavigation } from "@/modules/contacts/components/contacts-secondary-navigation";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";

// Same shell as the contacts tab — only the active tab and the table's columns differ.
const Loading = async () => {
  const t = await getTranslate();

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.contacts")}>
        <ContactsSecondaryNavigation activeId="attributes" loading />
      </PageHeader>
      <div className="mt-4 rounded-xl border border-slate-200 bg-white shadow-xs">
        <div className="grid h-12 grid-cols-6 content-center border-b border-slate-200 px-6 text-left text-sm font-semibold text-slate-900">
          <div className="col-span-3">{t("common.name")}</div>
          <div className="col-span-2 hidden sm:block">{t("common.description")}</div>
          <div className="col-span-1 hidden text-center sm:block">{t("common.updated_at")}</div>
        </div>
        {[...Array(5)].map((_, index) => (
          <div
            key={`attribute-loading-${index.toString()}`}
            className="m-2 grid h-14 animate-pulse grid-cols-6 content-center gap-x-4 rounded-lg px-4">
            <div className="col-span-3 h-4 rounded-full bg-slate-200"></div>
            <div className="col-span-2 hidden h-4 rounded-full bg-slate-200 sm:block"></div>
            <div className="col-span-1 hidden h-4 rounded-full bg-slate-200 sm:block"></div>
          </div>
        ))}
      </div>
    </PageContentWrapper>
  );
};

export default Loading;
