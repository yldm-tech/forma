import { useSuspenseQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { PageContentWrapper, PageHeader } from "../../components/ui/page";
import { billingPageQuery } from "../../lib/queries/billing";
import { type BillingSearch, PricingTable } from "./components/pricing-table";

/** forma2's billing settings page (modules/billing/page.tsx), on Forma Cloud only: the route turns the server's not-found elsewhere into the not-found page. */
export const BillingSettingsPage = ({
  organizationId,
  search,
}: {
  organizationId: string;
  search: BillingSearch;
}) => {
  const { t } = useTranslation();
  const { data: page } = useSuspenseQuery(billingPageQuery(organizationId));
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.billing")} />
      <PricingTable organizationId={organizationId} page={page} search={search} />
    </PageContentWrapper>
  );
};
