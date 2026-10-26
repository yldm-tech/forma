import { useMatches } from "@tanstack/react-router";

declare module "@tanstack/react-router" {
  interface StaticDataRouteOption {
    /** The page opens on a phone-sized screen, read-only, instead of under the phone overlay. */
    phoneReady?: boolean;
  }
}

/** The static data of a page a phone may open: the survey list, a survey's summary and responses, the contacts and a contact. Everything else (the editor, workflows, integrations, settings) stays under the overlay below the sm breakpoint. */
export const PHONE_READY = { phoneReady: true } as const;

/** Whether the page on screen is one a phone may open: the deepest route matched marks itself with PHONE_READY. */
export const useIsPhoneReadyPage = (): boolean =>
  useMatches({ select: (matches) => matches.at(-1)?.staticData.phoneReady === true });
