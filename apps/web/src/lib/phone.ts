import { useMatches } from "@tanstack/react-router";

declare module "@tanstack/react-router" {
  interface StaticDataRouteOption {
    /** The page opens on a phone-sized screen instead of under the phone overlay: read-only, except the email unsubscribe page, which flips the one notification switch its link names. */
    phoneReady?: boolean;
  }
}

/** The static data of a page a phone may open: the survey list, a survey's summary and responses, the contacts and a contact, read-only, and the email unsubscribe page (/account/unsubscribe), which turns off or back on the one notification switch its link names. Everything else (the editor, workflows, integrations, settings) stays under the overlay below the sm breakpoint. */
export const PHONE_READY = { phoneReady: true } as const;

/** Whether the page on screen is one a phone may open: the deepest route matched marks itself with PHONE_READY. */
export const useIsPhoneReadyPage = (): boolean =>
  useMatches({ select: (matches) => matches.at(-1)?.staticData.phoneReady === true });
