import { ITEMS_PER_PAGE } from "@/lib/constants";
import { getTranslate } from "@/lingodotdev/server";
import { ContactsPageLayout } from "@/modules/contacts/components/contacts-page-layout";
import { UploadContactsCSVButton } from "@/modules/contacts/components/upload-contacts-button";
import { getContactAttributeKeys } from "@/modules/contacts/lib/contact-attribute-keys";
import { getContacts } from "@/modules/contacts/lib/contacts";
import { getIsContactsEnabled, getIsQuotasEnabled } from "@/modules/license-check/lib/utils";
import { getWorkspaceAuth } from "@/modules/workspaces/lib/utils";
import { ContactDataView } from "./components/contact-data-view";

export const ContactsPage = async ({ params: paramsProps }: { params: Promise<{ workspaceId: string }> }) => {
  const params = await paramsProps;

  const { isReadOnly, workspace } = await getWorkspaceAuth(params.workspaceId);

  const t = await getTranslate();

  const isContactsEnabled = await getIsContactsEnabled();

  const isQuotasAllowed = await getIsQuotasEnabled();

  const contactAttributeKeys = isContactsEnabled ? await getContactAttributeKeys(workspace.id) : [];
  const initialContacts = isContactsEnabled ? await getContacts(workspace.id, 0) : [];

  const AddContactsButton = (
    <UploadContactsCSVButton workspaceId={workspace.id} contactAttributeKeys={contactAttributeKeys} />
  );

  return (
    <ContactsPageLayout
      pageTitle={t("common.contacts")}
      activeId="contacts"
      workspaceId={params.workspaceId}
      isContactsEnabled={isContactsEnabled}
      isReadOnly={isReadOnly}
      cta={AddContactsButton}>
      <ContactDataView
        workspaceId={workspace.id}
        itemsPerPage={ITEMS_PER_PAGE}
        contactAttributeKeys={contactAttributeKeys}
        isReadOnly={isReadOnly}
        initialContacts={initialContacts}
        hasMore={initialContacts.length >= ITEMS_PER_PAGE}
        isQuotasAllowed={isQuotasAllowed}
      />
    </ContactsPageLayout>
  );
};
