import type { TSurveySummary } from "@forma/types/surveys/types";
import { type ReactNode, createContext, useContext, useMemo } from "react";
import { buildElementImpressionLookup } from "../../lib/summary";

/** Per-element impressions for the one header that renders them, so they need not be threaded through every card. */
const ElementImpressionsContext = createContext<ReadonlyMap<string, number> | undefined>(undefined);

export const ElementImpressionsProvider = ({
  dropOff,
  children,
}: {
  dropOff: TSurveySummary["dropOff"];
  children: ReactNode;
}) => {
  const impressions = useMemo(() => buildElementImpressionLookup(dropOff), [dropOff]);
  return (
    <ElementImpressionsContext.Provider value={impressions}>{children}</ElementImpressionsContext.Provider>
  );
};

/** The element's impressions, or undefined outside a provider, which keeps the badge hidden. */
export const useElementImpressions = (elementId: string): number | undefined =>
  useContext(ElementImpressionsContext)?.get(elementId);
