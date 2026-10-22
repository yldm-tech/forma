import type { ElementType, ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { TooltipRenderer } from "../../../../components/ui/tooltip";
import { cn } from "../../../../lib/cn";
import { LinkTabsType, type ShareSettingsType, type ShareViaType } from "./share-types";
import { TabContainer } from "./tab-container";

export interface ShareTab {
  id: ShareViaType | ShareSettingsType;
  type: LinkTabsType;
  label: string;
  icon: ElementType;
  title: string;
  description: string;
  content: ReactNode;
  disabled?: boolean;
}

/** forma2's ShareView: the share channels and settings in a sidebar, the active one beside it; on small screens an icon row replaces the sidebar. */
export const ShareView = ({
  tabs,
  activeId,
  setActiveId,
}: {
  tabs: ShareTab[];
  activeId: ShareTab["id"];
  setActiveId: (id: ShareTab["id"]) => void;
}) => {
  const { t } = useTranslation();
  const activeTab = tabs.find((tab) => tab.id === activeId);
  const groups = [
    {
      id: LinkTabsType.SHARE_VIA,
      label: t("workspace.surveys.share.share_view_title"),
      tabs: tabs.filter((tab) => tab.type === LinkTabsType.SHARE_VIA),
    },
    {
      id: LinkTabsType.SHARE_SETTING,
      label: t("workspace.surveys.share.share_settings_title"),
      tabs: tabs.filter((tab) => tab.type === LinkTabsType.SHARE_SETTING),
    },
  ];

  return (
    <div className="flex h-full lg:grid lg:min-h-[620px] lg:grid-cols-4">
      <nav
        className="hidden h-full flex-col gap-4 rounded-l-lg border-r border-slate-200 bg-white p-4 lg:col-span-1 lg:flex"
        aria-label={t("workspace.surveys.summary.share_survey")}>
        {groups.map((group) => (
          <div key={group.id} className="flex flex-col gap-1">
            <p className="px-2 pb-1 text-xs text-slate-500">{group.label}</p>
            {group.tabs.map((tab) => (
              <button
                key={tab.id}
                type="button"
                onClick={() => setActiveId(tab.id)}
                disabled={tab.disabled}
                aria-current={tab.id === activeId ? "page" : undefined}
                className={cn(
                  "flex w-full items-center gap-2 rounded-md p-2 text-left text-sm hover:bg-slate-100 hover:text-slate-900 disabled:cursor-not-allowed disabled:opacity-50",
                  tab.id === activeId && !tab.disabled
                    ? "bg-slate-100 font-medium text-slate-900"
                    : "text-slate-700"
                )}>
                <tab.icon className="size-4 shrink-0 text-slate-700" />
                <span>{tab.label}</span>
              </button>
            ))}
          </div>
        ))}
      </nav>
      <div className="h-full w-full grow overflow-y-auto rounded-lg bg-slate-50 px-4 py-6 lg:col-span-3 lg:p-6">
        {activeTab && (
          <TabContainer key={activeTab.id} title={activeTab.title} description={activeTab.description}>
            {activeTab.content}
          </TabContainer>
        )}
        <div className="flex justify-center gap-2 rounded-md pt-6 text-center lg:hidden">
          {tabs.map((tab) => (
            <TooltipRenderer tooltipContent={tab.label} key={tab.id}>
              <Button
                variant="ghost"
                onClick={() => setActiveId(tab.id)}
                disabled={tab.disabled}
                aria-label={tab.label}
                className={cn(
                  "rounded-md px-4 py-2",
                  tab.id === activeId && !tab.disabled
                    ? "bg-white text-slate-900 shadow-xs hover:bg-white"
                    : "border-transparent text-slate-700 hover:text-slate-900"
                )}>
                <tab.icon className="size-4 text-slate-700" />
              </Button>
            </TooltipRenderer>
          ))}
        </div>
      </div>
    </div>
  );
};
