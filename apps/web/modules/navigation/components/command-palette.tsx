"use client";

import { SearchIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { getNavigationDestinations } from "@/modules/navigation/lib/destinations";
import { listSurveys } from "@/modules/survey/list/lib/v3-surveys-client";
import type { TSurveyListItem } from "@/modules/survey/list/types/survey-overview";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/modules/ui/components/command";

interface CommandPaletteProps {
  workspaceId: string;
  organizationId: string;
  isBilling: boolean;
  isOwnerOrManager: boolean;
  isFormaCloud: boolean;
}

const SURVEY_RESULT_LIMIT = 5;
const SEARCH_DEBOUNCE_MS = 250;

/**
 * One keystroke to anywhere, including a survey by name.
 *
 * The sidebar shows three areas because that is what the product has at the top level; everything
 * else — eight workspace settings pages, the organization's, the personal ones — is two clicks
 * down, and a survey is three. That depth is what makes the app read as smaller than it is, and a
 * palette is the cheapest way to make it legible without hanging more chrome off the sidebar.
 *
 * Survey results come from the list endpoint's existing `filter[name][contains]`, so this adds no
 * API surface. Destinations come from one shared list rather than from the sidebars, which each
 * know only their own slice.
 */
export const CommandPalette = ({
  workspaceId,
  organizationId,
  isBilling,
  isOwnerOrManager,
  isFormaCloud,
}: Readonly<CommandPaletteProps>) => {
  const { t } = useTranslation();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [surveys, setSurveys] = useState<TSurveyListItem[]>([]);

  const destinations = useMemo(
    () =>
      getNavigationDestinations({
        t,
        workspaceId,
        organizationId,
        isBilling,
        isOwnerOrManager,
        isFormaCloud,
      }),
    [t, workspaceId, organizationId, isBilling, isOwnerOrManager, isFormaCloud]
  );

  const groups = useMemo(() => {
    const byGroup = new Map<string, typeof destinations>();
    for (const destination of destinations) {
      byGroup.set(destination.group, [...(byGroup.get(destination.group) ?? []), destination]);
    }
    return [...byGroup.entries()];
  }, [destinations]);

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

  // A survey lookup is a request, so it waits for a pause in typing and for the palette to be open.
  // The billing role has no survey access, so it never asks.
  useEffect(() => {
    if (!open || isBilling || query.trim().length < 2) {
      setSurveys([]);
      return;
    }

    const controller = new AbortController();
    const timer = setTimeout(() => {
      listSurveys({
        workspaceId,
        limit: SURVEY_RESULT_LIMIT,
        includeTotalCount: false,
        filters: { name: query.trim(), status: [], type: [], sortBy: "relevance" },
        signal: controller.signal,
      })
        .then((page) => setSurveys(page.data))
        .catch(() => {
          // A failed lookup leaves the destinations usable; there is nothing to tell the user that
          // an empty result would not already say.
        });
    }, SEARCH_DEBOUNCE_MS);

    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [open, query, workspaceId, isBilling]);

  const go = useCallback(
    (href: string) => {
      setOpen(false);
      setQuery("");
      router.push(href);
    },
    [router]
  );

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={t("common.search")}
        className="flex items-center gap-2 rounded-md border border-slate-200 bg-white px-3 py-1.5 text-sm text-slate-500 transition-colors hover:border-slate-300 hover:text-slate-700 focus-visible:ring-2 focus-visible:ring-slate-500 focus-visible:outline-none">
        <SearchIcon className="size-4" strokeWidth={1.5} />
        <span className="hidden sm:inline">{t("common.search")}</span>
        <kbd className="hidden rounded border border-slate-200 bg-slate-50 px-1.5 font-sans text-xs text-slate-400 sm:inline">
          ⌘K
        </kbd>
      </button>

      <CommandDialog open={open} onOpenChange={setOpen}>
        <CommandInput placeholder={t("common.search")} value={query} onValueChange={setQuery} />
        <CommandList>
          <CommandEmpty>{t("common.no_results")}</CommandEmpty>

          {surveys.length > 0 && (
            <CommandGroup heading={t("common.surveys")}>
              {surveys.map((survey) => (
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
      </CommandDialog>
    </>
  );
};
