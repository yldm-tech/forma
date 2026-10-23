import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { SearchIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { surveysApi } from "../../features/surveys/api";
import { useDebouncedValue } from "../../lib/use-debounced-value";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "../ui/command";
import { Dialog, DialogTitle } from "../ui/dialog";

/** A place in the product the palette can send someone, forma2's NavigationDestination. */
export interface NavigationDestination {
  id: string;
  label: string;
  href: string;
  /** The heading it sits under. */
  group: string;
  /** Extra words to match on, for destinations whose label is not what someone would type. */
  keywords?: string[];
}

interface PaletteAccess {
  workspaceId: string;
  organizationId: string;
  /** The billing role holds no product access, so it is offered only what it can open. */
  isBilling: boolean;
  isOwnerOrManager: boolean;
  isFormaCloud: boolean;
}

/**
 * forma2's getNavigationDestinations: every place a person can be sent, as data, labelled with the keys the sidebars use. forma2's organization domain page is not offered: this server has no such page.
 */
export const navigationDestinations = (
  t: TFunction,
  { workspaceId, organizationId, isBilling, isOwnerOrManager, isFormaCloud }: PaletteAccess
): NavigationDestination[] => {
  const workspace = `/workspaces/${workspaceId}`;
  const workspaceSettings = `${workspace}/settings/workspace`;
  const organizationSettings = `/organizations/${organizationId}/settings`;
  const workspaceGroup = t("common.workspace");
  const settingsGroup = t("common.settings");
  const organizationGroup = t("common.organization");
  const accountGroup = t("common.account");

  const product: NavigationDestination[] = isBilling
    ? []
    : [
        { id: "surveys", label: t("common.surveys"), href: `${workspace}/surveys`, group: workspaceGroup },
        { id: "contacts", label: t("common.contacts"), href: `${workspace}/contacts`, group: workspaceGroup },
        {
          id: "workflows",
          label: t("common.workflows"),
          href: `${workspace}/workflows`,
          group: workspaceGroup,
        },
        {
          id: "user-actions",
          label: t("common.user_actions"),
          href: `${workspace}/user-actions`,
          group: workspaceGroup,
        },
        {
          id: "integrations",
          label: t("common.integrations"),
          href: `${workspace}/integrations`,
          group: workspaceGroup,
          keywords: ["zapier", "webhook", "slack", "notion", "airtable", "google sheets", "n8n", "make"],
        },
        {
          id: "app-connection",
          label: t("common.web_and_mobile_sdk"),
          href: `${workspace}/integrations/app-connection`,
          group: workspaceGroup,
          keywords: ["sdk", "javascript", "snippet", "embed"],
        },
        { id: "tags", label: t("common.tags"), href: `${workspace}/tags`, group: workspaceGroup },
      ];

  const workspaceSettingsItems: NavigationDestination[] = isBilling
    ? []
    : [
        {
          id: "ws-general",
          label: t("common.general"),
          href: `${workspaceSettings}/general`,
          group: settingsGroup,
        },
        {
          id: "ws-look",
          label: t("common.appearance"),
          href: `${workspaceSettings}/look`,
          group: settingsGroup,
        },
        {
          id: "ws-languages",
          label: t("common.survey_languages"),
          href: `${workspaceSettings}/languages`,
          group: settingsGroup,
        },
        {
          id: "ws-teams",
          label: t("common.team_access"),
          href: `${workspaceSettings}/teams`,
          group: settingsGroup,
        },
      ];

  const organizationItems: NavigationDestination[] = [
    {
      id: "org-general",
      label: t("common.general"),
      href: `${organizationSettings}/general`,
      group: organizationGroup,
    },
    {
      id: "org-teams",
      label: t("common.teams"),
      href: `${organizationSettings}/teams`,
      group: organizationGroup,
    },
    ...(isOwnerOrManager
      ? [
          {
            id: "org-api-keys",
            label: t("common.api_keys"),
            href: `${organizationSettings}/api-keys`,
            group: organizationGroup,
          },
        ]
      : []),
    // The billing page exists on Forma Cloud only.
    ...(isFormaCloud
      ? [
          {
            id: "org-billing",
            label: t("common.billing"),
            href: `${organizationSettings}/billing`,
            group: organizationGroup,
          },
        ]
      : []),
  ];

  const account: NavigationDestination[] = [
    {
      id: "account-profile",
      label: t("common.your_profile"),
      href: "/account/settings/profile",
      group: accountGroup,
    },
    ...(isBilling
      ? []
      : [
          {
            id: "account-notifications",
            label: t("common.notifications"),
            href: "/account/settings/notifications",
            group: accountGroup,
          },
        ]),
  ];

  return [...product, ...workspaceSettingsItems, ...organizationItems, ...account];
};

const SURVEY_RESULT_LIMIT = 5;
const SEARCH_DEBOUNCE_MS = 250;

/**
 * forma2's CommandPalette: Cmd/Ctrl+K, or the search button, opens one search over every destination and the workspace's surveys by name. The survey lookup is the list endpoint's filter[name][contains], asked after a pause in typing, and never for the billing role, which has no survey access.
 */
export const CommandPalette = (access: PaletteAccess) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const term = useDebouncedValue(query.trim(), SEARCH_DEBOUNCE_MS);
  const { workspaceId, organizationId, isBilling, isOwnerOrManager, isFormaCloud } = access;

  const destinations = useMemo(
    () =>
      navigationDestinations(t, { workspaceId, organizationId, isBilling, isOwnerOrManager, isFormaCloud }),
    [t, workspaceId, organizationId, isBilling, isOwnerOrManager, isFormaCloud]
  );
  const groups = useMemo(() => {
    const byGroup = new Map<string, NavigationDestination[]>();
    for (const destination of destinations) {
      byGroup.set(destination.group, [...(byGroup.get(destination.group) ?? []), destination]);
    }
    return [...byGroup.entries()];
  }, [destinations]);

  const searching = open && !isBilling && term.length >= 2;
  const surveys = useQuery({
    queryKey: ["command-palette", "surveys", workspaceId, term],
    queryFn: ({ signal }) =>
      surveysApi.list(
        {
          workspaceId,
          limit: SURVEY_RESULT_LIMIT,
          includeTotalCount: false,
          filters: { name: term, status: [], type: [], sortBy: "relevance" },
        },
        signal
      ),
    enabled: searching,
  });
  // A failed lookup leaves the destinations usable; an empty result says the rest.
  const surveyResults = searching ? (surveys.data?.data ?? []) : [];

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "k" && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        setOpen((previous) => !previous);
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  const go = (href: string) => {
    setOpen(false);
    setQuery("");
    void navigate({ href });
  };

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={t("common.search")}
        className="flex items-center gap-2 rounded-md border border-slate-200 bg-white px-3 py-1.5 text-sm text-slate-500 transition-colors hover:border-slate-300 hover:text-slate-700 focus-visible:ring-2 focus-visible:ring-slate-500 focus-visible:outline-none">
        <SearchIcon className="size-4" strokeWidth={1.5} aria-hidden="true" />
        <span className="hidden sm:inline">{t("common.search")}</span>
        <kbd className="hidden rounded border border-slate-200 bg-slate-50 px-1.5 font-sans text-xs text-slate-400 sm:inline">
          ⌘K
        </kbd>
      </button>

      <Dialog open={open} onOpenChange={setOpen} width="narrow" hideCloseButton>
        <DialogTitle className="sr-only">{t("common.search")}</DialogTitle>
        <Command>
          <CommandInput
            placeholder={t("common.search")}
            value={query}
            onValueChange={setQuery}
            className="border-b border-slate-200 px-3"
          />
          <CommandList>
            <CommandEmpty>{t("common.no_results")}</CommandEmpty>
            {surveyResults.length > 0 && (
              <CommandGroup heading={t("common.surveys")}>
                {surveyResults.map((survey) => (
                  <CommandItem
                    key={survey.id}
                    value={`survey-${survey.id}-${survey.name}`}
                    onSelect={() => go(`/workspaces/${workspaceId}/surveys/${survey.id}/summary`)}>
                    {survey.name}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {groups.map(([heading, items]) => (
              <CommandGroup key={heading} heading={heading}>
                {items.map((destination) => (
                  <CommandItem
                    key={destination.id}
                    value={`${heading} ${destination.label} ${destination.keywords?.join(" ") ?? ""}`}
                    onSelect={() => go(destination.href)}>
                    {destination.label}
                  </CommandItem>
                ))}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
      </Dialog>
    </>
  );
};
