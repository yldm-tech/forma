import { queryOptions, useQuery } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { ApiError, api } from "../../lib/api";

// forma2's PlainChat (apps/web/app/plain): Plain's in-product support chat, loaded only when the server has a Plain app configured. The server resolves who the chat identifies (apps/server/internal/plain), with the email hash Plain verifies the customer by, computed there because it is a bearer credential.

/** GET /api/app/plain/session: the widget's app id and the signed-in customer. */
export interface PlainSession {
  appId: string;
  userId: string;
  email: string;
  name: string;
  /** HMAC-SHA256 of the email, null when the chat runs unverified. */
  emailHash: string | null;
  /** The Plain label applied to every thread a paying customer opens, resolved before the widget starts. */
  activeCustomerLabelTypeId: string | null;
}

/** The session, or null when Plain is not configured (the server answers 404), in which case nothing is loaded. */
export const plainSessionQuery = queryOptions({
  queryKey: ["plain", "session"] as const,
  queryFn: async ({ signal }): Promise<PlainSession | null> => {
    try {
      return await api.get<PlainSession>("/api/app/plain/session", { signal });
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) return null;
      throw error;
    }
  },
  staleTime: Number.POSITIVE_INFINITY,
  retry: false,
});

export const PLAIN_SCRIPT_ID = "plain-chat-script";
export const PLAIN_SCRIPT_SRC = "https://chat.cdn-plain.com/index.js";

// Forma brand crimson, for the chat panel's accents.
const BRAND_COLOR = "#D3264A";

interface PlainCustomerDetails {
  email: string;
  emailHash?: string;
  fullName?: string;
  shortName?: string;
  externalId?: string;
}

interface PlainInitOptions {
  appId: string;
  hideLauncher?: boolean;
  theme?: "auto" | "light" | "dark";
  style?: { brandColor?: string; launcherBackgroundColor?: string; launcherIconColor?: string };
  customerDetails?: PlainCustomerDetails;
  threadDetails?: { labelTypeIds?: string[] };
}

interface PlainInstance {
  init: (options: PlainInitOptions) => void;
  update: (options: Partial<PlainInitOptions>) => void;
  setCustomerDetails: (details: PlainCustomerDetails) => void;
  close: () => void;
  isInitialized: () => boolean;
}

const getPlain = (): PlainInstance | null => (globalThis as { Plain?: PlainInstance }).Plain ?? null;

const customerDetails = (session: PlainSession): PlainCustomerDetails => ({
  email: session.email,
  emailHash: session.emailHash ?? undefined,
  fullName: session.name || undefined,
  shortName: session.name ? session.name.split(" ")[0] : undefined,
  externalId: session.userId,
});

const threadDetails = (session: PlainSession) =>
  session.activeCustomerLabelTypeId ? { labelTypeIds: [session.activeCustomerLabelTypeId] } : undefined;

/** Loads Plain's script once and initializes the widget for the customer; later identity and label changes go through Plain's in-place updates, so the script is never injected twice. */
const PlainWidget = ({ session }: { session: PlainSession }) => {
  // The script's onload reads the latest options without re-registering the loader on every change.
  const initOptions = useRef<PlainInitOptions>({ appId: session.appId });
  useEffect(() => {
    initOptions.current = {
      appId: session.appId,
      theme: "auto",
      style: { brandColor: BRAND_COLOR, launcherBackgroundColor: BRAND_COLOR, launcherIconColor: "#FFFFFF" },
      customerDetails: customerDetails(session),
      threadDetails: threadDetails(session),
    };
  });

  useEffect(() => {
    if (!document.getElementById(PLAIN_SCRIPT_ID)) {
      const script = document.createElement("script");
      script.src = PLAIN_SCRIPT_SRC;
      script.id = PLAIN_SCRIPT_ID;
      script.async = true;
      script.onload = () => {
        const plain = getPlain();
        if (plain && !plain.isInitialized()) plain.init(initOptions.current);
      };
      document.head.appendChild(script);
    }
    // Closed on unmount (sign-out); the identity effect overwrites the customer on an account switch, so no earlier user's details carry over.
    return () => getPlain()?.close();
  }, []);

  // A new identity or label goes into the widget already running instead of re-initializing it.
  useEffect(() => {
    const plain = getPlain();
    if (!plain?.isInitialized()) return;
    plain.setCustomerDetails(customerDetails(session));
    plain.update({ threadDetails: threadDetails(session) });
  }, [session]);

  return null;
};

/** forma2's PlainChat, mounted by the app shell: renders nothing, and loads nothing unless the server configured Plain. */
export const PlainChat = () => {
  const session = useQuery(plainSessionQuery).data;
  return session ? <PlainWidget session={session} /> : null;
};
