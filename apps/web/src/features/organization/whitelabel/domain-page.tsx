import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../../components/ui/alert";
import { IdBadge } from "../../../components/ui/id-badge";
import { PageContentWrapper, PageHeader, SettingsCard } from "../../../components/ui/page";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../../components/ui/table";
import { instanceQuery } from "../../../lib/queries/instance";
import type { OrganizationDetail } from "../../../lib/queries/organizations";
import type { PrettyUrlSurvey } from "./api";
import { FaviconCustomizationCard } from "./favicon-customization-card";

/** forma2's PrettyUrlsTable: each survey with a pretty URL, its workspace, and the URL to copy. */
const PrettyUrlsTable = ({ surveys }: { surveys: PrettyUrlSurvey[] }) => {
  const { t } = useTranslation();
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-[40%]">{t("workspace.settings.domain.survey_name")}</TableHead>
          <TableHead className="w-[30%]">{t("workspace.settings.domain.workspace")}</TableHead>
          <TableHead className="w-[30%]">{t("workspace.settings.domain.pretty_url")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {surveys.length === 0 ? (
          <TableRow>
            <TableCell colSpan={3} className="text-center text-slate-500">
              {t("workspace.settings.domain.no_pretty_urls")}
            </TableCell>
          </TableRow>
        ) : (
          surveys.map((survey) => (
            <TableRow key={survey.id}>
              <TableCell className="font-medium">
                <Link
                  to="/workspaces/$workspaceId/surveys/$surveyId/summary"
                  params={{ workspaceId: survey.workspace.id, surveyId: survey.id }}
                  className="text-slate-900 hover:text-slate-700 hover:underline">
                  {survey.name}
                </Link>
              </TableCell>
              <TableCell>{survey.workspace.name}</TableCell>
              <TableCell>
                <IdBadge id={survey.slug} />
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  );
};

/** forma2's organization domain settings page, self-hosted only: the storage warning, the survey favicon, and the organization's pretty URLs. */
export const DomainSettingsPage = ({
  organization,
  surveys,
}: {
  organization: OrganizationDetail;
  surveys: PrettyUrlSurvey[];
}) => {
  const { t } = useTranslation();
  const { data: instance } = useQuery(instanceQuery);
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.domain")} />
      {instance?.storageConfigured === false && (
        <div className="max-w-4xl">
          <Alert variant="warning" role="status">
            <AlertDescription>{t("common.storage_not_configured")}</AlertDescription>
          </Alert>
        </div>
      )}
      <FaviconCustomizationCard key={organization.updatedAt} organization={organization} />
      <SettingsCard
        width="full"
        title={t("workspace.settings.domain.title")}
        description={t("workspace.settings.domain.description")}
        bodyVariant="flush">
        <PrettyUrlsTable surveys={surveys} />
      </SettingsCard>
    </PageContentWrapper>
  );
};
