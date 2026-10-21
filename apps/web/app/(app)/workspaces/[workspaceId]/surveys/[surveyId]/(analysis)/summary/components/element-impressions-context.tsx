"use client";

import { type ReactNode, createContext, useContext, useMemo } from "react";
import type { TSurveySummary } from "@forma/types/surveys/types";
import { buildElementImpressionLookup } from "@/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/summary/lib/element-denominator";

/**
 * Per-element impressions, shared with the one component that renders them. Every element card funnels
 * through `ElementSummaryHeader`, but the cards themselves are seventeen separate components, so a prop
 * would have to be threaded through all of them to reach it. The value is read-only and derived from
 * the summary the page already holds.
 */
const ElementImpressionsContext = createContext<ReadonlyMap<string, number> | undefined>(undefined);

export const ElementImpressionsProvider = ({
  dropOff,
  children,
}: Readonly<{ dropOff: TSurveySummary["dropOff"]; children: ReactNode }>) => {
  const impressions = useMemo(() => buildElementImpressionLookup(dropOff), [dropOff]);

  return (
    <ElementImpressionsContext.Provider value={impressions}>{children}</ElementImpressionsContext.Provider>
  );
};

/**
 * Returns the element's impressions, or `undefined` outside a provider — the header is also rendered by
 * the summary email preview and by any future surface that has no drop-off data, and those keep the
 * badge hidden rather than throwing.
 */
export const useElementImpressions = (elementId: string): number | undefined =>
  useContext(ElementImpressionsContext)?.get(elementId);
