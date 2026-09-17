import { notFound } from "next/navigation";
import { ReactNode } from "react";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";
import { ContactsSecondaryNavigation } from "./contacts-secondary-navigation";

interface ContactsPageLayoutProps {
  pageTitle: string;
  activeId: string;
  workspaceId: string;
  isContactsEnabled: boolean;
  isReadOnly: boolean;
  cta?: ReactNode;
  children: ReactNode;
}

export const ContactsPageLayout = async ({
  pageTitle,
  activeId,
  workspaceId,
  isContactsEnabled,
  isReadOnly,
  cta,
  children,
}: ContactsPageLayoutProps) => {
  // Not entitled: this installation does not have contacts, so the route does not exist for it. The
  // nav already omits the entry, which leaves old links and bookmarks — and answering those with a
  // page whose only content is an upsell reads as a broken product rather than a smaller one.
  if (!isContactsEnabled) {
    notFound();
  }

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={pageTitle} cta={isReadOnly ? undefined : cta}>
        <ContactsSecondaryNavigation activeId={activeId} workspaceId={workspaceId} />
      </PageHeader>

      {children}
    </PageContentWrapper>
  );
};
