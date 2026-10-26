import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { PageContentWrapper, PageHeader } from "../../components/ui/page";
import { SecondaryNavigation } from "../../components/ui/secondary-navigation";

/** forma2's ContactsPageLayout: the page title, the action read-only users do not get, and the contacts, attributes and segments tabs, which showSections false leaves out. */
export const ContactsPageLayout = ({
  pageTitle,
  activeId,
  workspaceId,
  isReadOnly,
  showSections = true,
  cta,
  children,
}: {
  pageTitle: string;
  activeId: "contacts" | "attributes" | "segments";
  workspaceId: string;
  isReadOnly: boolean;
  showSections?: boolean;
  cta?: ReactNode;
  children: ReactNode;
}) => {
  const { t } = useTranslation();
  const base = `/workspaces/${workspaceId}`;
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={pageTitle} cta={isReadOnly ? undefined : cta}>
        {showSections && (
          <SecondaryNavigation
            ariaLabel={t("common.contacts")}
            activeId={activeId}
            navigation={[
              { id: "contacts", label: t("common.contacts"), href: `${base}/contacts` },
              { id: "attributes", label: t("common.attributes"), href: `${base}/attributes` },
              { id: "segments", label: t("common.segments"), href: `${base}/segments` },
            ]}
          />
        )}
      </PageHeader>
      {children}
    </PageContentWrapper>
  );
};
