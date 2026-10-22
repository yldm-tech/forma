import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { PageContentWrapper, PageHeader } from "../../../../../components/ui/page";
import { MembersCard } from "../../../../../features/organization/members";
import { TeamsCard } from "../../../../../features/organization/teams";
import { organizationQuery } from "../../../../../lib/queries/organizations";

// forma2's teams page: the organization's members and invites, then its teams.
export const Route = createFileRoute("/_authenticated/organizations/$organizationId/settings/teams")({
  component: TeamsSettings,
});

function TeamsSettings() {
  const { t } = useTranslation();
  const { organizationId } = Route.useParams();
  const { data: organization } = useSuspenseQuery(organizationQuery(organizationId));
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.teams")} />
      <MembersCard organization={organization} />
      <TeamsCard organization={organization} />
    </PageContentWrapper>
  );
}
