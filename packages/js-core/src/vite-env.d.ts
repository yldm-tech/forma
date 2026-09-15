/// <reference types="vite/client" />

declare global {
  interface Window {
    /** GTM's data layer. We push via the standard `window.dataLayer = window.dataLayer || []` idiom. */
    dataLayer?: Record<string, unknown>[];
    __formaNonce?: string;
    formaSurveys?: {
      renderSurvey: (options: unknown) => void;
      // Optional: the surveys bundle is served by the (possibly self-hosted, older)
      // Forma instance, so it may predate setNonce.
      setNonce?: (nonce: string | undefined) => void;
    };
  }
}

export {};
