import { createContext, useContext } from "react";
import type { SiteContent } from "../content";
import type { Locale, PageId } from "../site";

export interface SiteContextValue {
  locale: Locale;
  /** The page being rendered; undefined on the 404 page. */
  page: PageId | undefined;
  content: SiteContent;
}

export const SiteContext = createContext<SiteContextValue | null>(null);

export const useSite = (): SiteContextValue => {
  const value = useContext(SiteContext);
  if (!value) throw new Error("useSite outside <SiteContext>");
  return value;
};
