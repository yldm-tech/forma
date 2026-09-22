import { getTranslate } from "@/lingodotdev/server";
import { LoadingCard } from "@/modules/ui/components/loading-card";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";
import { SettingsCardGrid } from "@/modules/ui/components/settings-card-grid";

const Loading = async () => {
  const t = await getTranslate();

  const cards = [
    {
      title: t("workspace.settings.general.organization_name"),
      description: t("workspace.settings.general.organization_name_description"),
      skeletonLines: [{ classes: "h-6 w-28" }, { classes: "h-8 w-80" }],
    },
    {
      title: t("workspace.settings.general.delete_organization"),
      description: t("workspace.settings.general.delete_organization_description"),
      skeletonLines: [{ classes: "h-6 w-28" }, { classes: "h-8 w-80" }],
    },
  ];

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("workspace.settings.general.organization_settings")} />
      <SettingsCardGrid>
        {cards.map((card, index) => (
          <LoadingCard key={index} {...card} width="full" />
        ))}
      </SettingsCardGrid>
    </PageContentWrapper>
  );
};

export default Loading;
