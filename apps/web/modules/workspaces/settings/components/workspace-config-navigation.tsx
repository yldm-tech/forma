"use client";

import { BrushIcon, CodeXmlIcon, LanguagesIcon, TagIcon, UsersIcon } from "lucide-react";
import { usePathname } from "next/navigation";
import { useTranslation } from "react-i18next";
import { SecondaryNavigation } from "@/modules/ui/components/secondary-navigation";
import { useWorkspace } from "@/modules/workspaces/context/workspace-context";

interface WorkspaceConfigNavigationProps {
  activeId: string;
  loading?: boolean;
}

export const WorkspaceConfigNavigation = ({ activeId, loading }: WorkspaceConfigNavigationProps) => {
  const { t } = useTranslation();
  const pathname = usePathname();
  const { workspace } = useWorkspace();
  const workspaceBasePath = `/workspaces/${workspace?.id}`;

  let navigation = [
    {
      id: "general",
      label: t("common.general"),
      icon: <UsersIcon className="size-5" />,
      href: `${workspaceBasePath}/settings/workspace/general`,
      current: pathname?.includes("/general"),
    },
    {
      id: "look",
      label: t("common.appearance"),
      icon: <BrushIcon className="size-5" />,
      href: `${workspaceBasePath}/settings/workspace/look`,
      current: pathname?.includes("/look"),
    },
    {
      id: "app-connection",
      label: t("common.web_and_mobile_sdk"),
      icon: <CodeXmlIcon className="size-5" />,
      href: `${workspaceBasePath}/integrations/app-connection`,
      current: pathname?.includes("/app-connection"),
    },
    {
      id: "teams",
      label: t("common.team_access"),
      icon: <UsersIcon className="size-5" />,
      href: `${workspaceBasePath}/settings/workspace/teams`,
      current: pathname?.includes("/teams"),
    },
    {
      id: "languages",
      label: t("common.survey_languages"),
      icon: <LanguagesIcon className="size-5" />,
      href: `${workspaceBasePath}/settings/workspace/languages`,
      current: pathname?.includes("/languages"),
    },
    {
      id: "tags",
      label: t("common.tags"),
      icon: <TagIcon className="size-5" />,
      href: `${workspaceBasePath}/tags`,
      current: pathname?.includes("/tags"),
    },
  ];

  return <SecondaryNavigation navigation={navigation} activeId={activeId} loading={loading} />;
};
