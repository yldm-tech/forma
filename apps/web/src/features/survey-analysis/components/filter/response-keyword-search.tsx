import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TSurvey } from "@forma/types/surveys/types";
import { SearchIcon, XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { getElementsFromBlocks } from "../../../survey-editor/lib/client-utils";
import { useResponseFilter } from "./response-filter-context";

/** How long typing waits before the responses are asked again. */
const KEYWORD_DEBOUNCE_MS = 400;

// The elements whose answers the server's keyword search reads (response.searchElementTypes): the respondent's own words.
const SEARCHABLE_ELEMENT_TYPES = new Set<string>([
  TSurveyElementTypeEnum.OpenText,
  TSurveyElementTypeEnum.Address,
  TSurveyElementTypeEnum.ContactInfo,
]);

/** Whether the survey has answers the keyword search can read. */
export const hasSearchableAnswers = (survey: TSurvey): boolean =>
  getElementsFromBlocks(survey.blocks).some((element) => SEARCHABLE_ELEMENT_TYPES.has(element.type));

/**
 * The keyword box over the response table: narrows the responses (and the summary, which shares the filter) to those whose free-text answers hold the typed text, ignoring case. It writes the shared filter's keyword, so the search is in the ?filter= link, shows as a chip and is cleared with the other filters.
 */
export const ResponseKeywordSearch = () => {
  const { t } = useTranslation();
  const { selectedFilter, setSelectedFilter } = useResponseFilter();
  const applied = selectedFilter.keyword ?? "";
  const [text, setText] = useState(applied);

  // A keyword changed from outside (its chip removed, Clear all, a link) replaces what is typed; the keyword this box applied itself leaves the text alone, trailing space and all, so typing goes on.
  const [lastApplied, setLastApplied] = useState(applied);
  if (lastApplied !== applied) {
    setLastApplied(applied);
    if (text.trim() !== applied) setText(applied);
  }

  useEffect(() => {
    if (text.trim() === applied.trim()) return;
    const timeout = setTimeout(
      () => setSelectedFilter((current) => ({ ...current, keyword: text.trim() || undefined })),
      KEYWORD_DEBOUNCE_MS
    );
    return () => clearTimeout(timeout);
  }, [text, applied, setSelectedFilter]);

  const label = t("workspace.analysis.search.placeholder");
  return (
    <div className="focus-within:ring-ring flex h-9 w-72 max-w-full items-center gap-2 rounded-md border border-slate-300 bg-white px-3 text-slate-700 focus-within:ring-2 focus-within:ring-offset-1 hover:border-slate-400">
      <SearchIcon className="size-4 shrink-0 text-slate-500" aria-hidden="true" />
      <input
        type="search"
        aria-label={label}
        className="w-full min-w-0 border-none bg-transparent text-sm focus:outline-hidden [&::-webkit-search-cancel-button]:hidden"
        placeholder={label}
        value={text}
        onChange={(event) => setText(event.target.value)}
        onKeyDown={(event) => {
          // Enter searches at once rather than after the pause.
          if (event.key === "Enter") {
            setSelectedFilter((current) => ({ ...current, keyword: text.trim() || undefined }));
          }
        }}
      />
      {text && (
        <button
          type="button"
          className="focus-visible:ring-ring shrink-0 cursor-pointer rounded-sm p-0.5 text-slate-500 hover:bg-slate-100 hover:text-slate-800 focus-visible:ring-2 focus-visible:outline-hidden"
          aria-label={t("workspace.analysis.search.clear")}
          onClick={() => {
            setText("");
            setSelectedFilter((current) => ({ ...current, keyword: undefined }));
          }}>
          <XIcon className="size-3.5" />
        </button>
      )}
    </div>
  );
};
