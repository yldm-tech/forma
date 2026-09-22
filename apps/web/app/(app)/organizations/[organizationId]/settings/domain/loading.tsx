import { getTranslate } from "@/lingodotdev/server";
import { LoadingCard } from "@/modules/ui/components/loading-card";
import { PageContentWrapper } from "@/modules/ui/components/page-content-wrapper";
import { PageHeader } from "@/modules/ui/components/page-header";

// The favicon card and the pretty-URL table, in that order, so the skeleton occupies the same two
// slots the resolved page does.
const Loading = async () => {
  const t = await getTranslate();

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.domain")} />
      <LoadingCard
        title={t("workspace.settings.domain.favicon_customization")}
        description={t("workspace.settings.domain.favicon_customization_description")}
        skeletonLines={[{ classes: "h-16 w-16" }, { classes: "h-8 w-56" }]}
        width="full"
      />
      <LoadingCard
        title={t("workspace.settings.domain.title")}
        description={t("workspace.settings.domain.description")}
        skeletonLines={[{ classes: "h-10 w-full" }, { classes: "h-10 w-full" }, { classes: "h-10 w-full" }]}
        width="full"
      />
    </PageContentWrapper>
  );
};

export default Loading;
