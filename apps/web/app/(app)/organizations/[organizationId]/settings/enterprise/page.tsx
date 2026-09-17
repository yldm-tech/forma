import { notFound, redirect } from "next/navigation";
import { EnterpriseLicenseFeaturesTable } from "@/app/(app)/workspaces/[workspaceId]/settings/organization/enterprise/components/EnterpriseLicenseFeaturesTable";
import { can } from "@/lib/authorization";
import { withAuthorizationSurface } from "@/lib/authorization/context";
import { IS_FORMA_CLOUD } from "@/lib/constants";
import { getTranslate } from "@/lingodotdev/server";
import { getEnterpriseLicense } from "@/modules/license-check/lib/license";
import { getOrganizationAuth } from "@/modules/organization/lib/utils";
import { getOrganizationBillingPath } from "@/modules/settings/lib/routes";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";

const Page = async (props: Readonly<{ params: Promise<{ organizationId: string }> }>) => {
  const params = await props.params;
  const t = await getTranslate();
  const { session, isBilling } = await getOrganizationAuth(params.organizationId);

  // Not routed through can(), deliberately: this line does not decide access. On Cloud the next
  // block refuses everyone anyway, so all it chooses is whether a billing-role user gets a 302 to
  // their billing home or a 404. Expressing it centrally would need a "billing role only"
  // capability, and inventing one would encode a role name as a permission.
  if (isBilling && IS_FORMA_CLOUD) {
    redirect(getOrganizationBillingPath(params.organizationId, IS_FORMA_CLOUD));
  }

  if (IS_FORMA_CLOUD) {
    return notFound();
  }

  // ENG-2409: was `isMember -> notFound()`.
  //
  // `organization.manage_billing` (owner + manager + billing) is exactly `!isMember` — the caller
  // has a membership by now, since getOrganizationAuth throws otherwise, so the four roles partition
  // cleanly. It is NOT `organization.manage`, which is the mapping this gate invites: on self-hosted
  // `getOrganizationBillingPath(orgId, false)` resolves to this very page, so this is where the
  // billing role gets redirected TO. Gating on owner + manager would 404 that role on its own
  // landing page, and billing-role-access.spec.ts would not catch it — it asserts on URL, and a 404
  // keeps the URL.
  const hasBillingAccess = await withAuthorizationSurface("page", () =>
    can({ type: "user", id: session.user.id }, "organization.manage_billing", {
      type: "organization",
      id: params.organizationId,
    })
  );

  if (!hasBillingAccess) {
    return notFound();
  }

  // Every feature is on, so this page is an inventory rather than a licence status. The upsell half
  // it used to carry — request a lite licence, start a trial, compare what a key would unlock — went
  // with the licence check itself.
  const { features } = await getEnterpriseLicense();

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.enterprise_license")} />
      {features && <EnterpriseLicenseFeaturesTable features={features} />}
    </PageContentWrapper>
  );
};

export default Page;
