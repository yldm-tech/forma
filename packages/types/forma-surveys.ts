import type { TJsFileUploadParams, TJsWorkspaceStateSurvey } from "./js";
import type { TResponseData, TResponseHiddenFieldValue, TResponseUpdate } from "./responses";
import type { TUploadFileConfig } from "./storage";
import type { TSurveyStyling } from "./surveys/types";
import type { TWorkspaceStyling } from "./workspace";

export interface SurveyBaseProps {
  survey: TJsWorkspaceStateSurvey;
  styling: TSurveyStyling | TWorkspaceStyling;
  isBrandingEnabled: boolean;
  getSetIsError?: (getSetError: (value: boolean) => void) => void;
  getSetIsResponseSendingFinished?: (getSetIsResponseSendingFinished: (value: boolean) => void) => void;
  getSetBlockId?: (getSetBlockId: (value: string) => void) => void;
  getSetResponseData?: (getSetResponseData: (value: TResponseData) => void) => void;
  onDisplay?: () => Promise<void>;
  onResponse?: (response: TResponseUpdate) => void;
  /**
   * Fires when the finished response has been sent. `responseId` is the persisted id when one exists
   * (it always does outside preview/offline, since this is gated on the send completing) — ENG-1846.
   */
  onFinished?: (responseId?: string) => void;
  onClose?: () => void;
  onRetry?: () => void;
  autoFocus?: boolean;
  isRedirectDisabled?: boolean;
  prefillResponseData?: TResponseData;
  skipPrefilled?: boolean;
  languageCode: string;
  dir?: "ltr" | "rtl" | "auto";
  setDir?: (dir: "ltr" | "rtl" | "auto") => void;
  /** Notifies the host of the survey's active language code (e.g. "default", "en-AU", "he").
   *  Link surveys use it to keep the page lang/dir in sync; embedded widgets omit it. */
  onLanguageChange?: (languageCode: string) => void;
  /** Notifies the host which card the respondent is on (initial position + every navigation), so a
   *  link survey can title the document per step (WCAG 2.4.2). `label` is pre-localized in the
   *  SURVEY's active language, which the host does not track — its own i18n is in the viewer's UI
   *  locale. Embedded widgets omit the callback, so a host page is never touched. */
  onPageChange?: (page: { index: number; total: number; label: string }) => void;
  onFileUpload: (file: TJsFileUploadParams["file"], config?: TUploadFileConfig) => Promise<string>;
  responseCount?: number;
  isCardBorderVisible?: boolean;
  startAtQuestionId?: string;
  clickOutside?: boolean;
  hiddenFieldsRecord?: TResponseHiddenFieldValue;
  shouldResetQuestionId?: boolean;
  fullSizeCards?: boolean;
  /**
   * The tallest a card's content may grow before it scrolls inside the card, as a CSS length. The link survey page derives it from the viewport, so a card uses the page instead of a fixed share of it, and keeps the card's Back/Next row in view at the card's bottom. Unset, a card keeps 60dvh (42dvh in the editor preview), which is what the in-app widget gets.
   */
  cardMaxHeight?: string;
  showCardlessPreviewLogoSlot?: boolean;
}

export interface SurveyInlineProps extends SurveyBaseProps {
  containerId: string;
}

export interface SurveyModalProps extends SurveyBaseProps {
  clickOutside: boolean;
  overlay: "none" | "light" | "dark";
  placement: "bottomLeft" | "bottomRight" | "topLeft" | "topRight" | "center";
}

export interface SurveyContainerProps extends Omit<SurveyBaseProps, "onFileUpload"> {
  appUrl?: string;
  workspaceId?: string;
  /** Legacy alias for `workspaceId`, sent by old SDKs (e.g. Android ≤ v1.2.0). */
  environmentId?: string;
  isPreviewMode?: boolean;
  userId?: string;
  contactId?: string;
  onDisplayCreated?: () => void | Promise<void>;
  /**
   * Fires once per survey lifecycle when the response exists. Outside preview mode that is the
   * server's creation ack, so `responseId` is the persisted id (ENG-1846 — the host uses it to link
   * session replays); in preview mode it fires at submit time with no id, since nothing is stored.
   */
  onResponseCreated?: (responseId?: string) => void | Promise<void>;
  onFileUpload?: (file: TJsFileUploadParams["file"], config?: TUploadFileConfig) => Promise<string>;
  onOpenExternalURL?: (url: string) => void | Promise<void>;
  mode?: "modal" | "inline";
  containerId?: string;
  overlay?: "none" | "light" | "dark";
  placement?: "bottomLeft" | "bottomRight" | "topLeft" | "topRight" | "center";
  action?: string;
  singleUseId?: string;
  singleUseResponseId?: string;
  pinAuthToken?: string;
  isWebEnvironment?: boolean;
  isSpamProtectionEnabled?: boolean;
  recaptchaSiteKey?: string;
  getRecaptchaToken?: () => Promise<string | null>;
  /**
   * Keeps a link survey's progress in the browser's IndexedDB (the answers, the card, the response and display ids, and the answers not yet sent) for 24 hours, so a reload goes on in the same response. The link page passes the survey's `isProgressSavingEnabled`. Unlike `offlineSupport`, it neither holds answers back while the browser is offline nor asks before the page is left.
   */
  persistProgress?: boolean;
  /**
   * The offline mode the `?offlineSupport=true` link parameter turns on: progress is kept as with `persistProgress`, answers wait while the browser reports it is offline, and leaving a survey half answered asks for confirmation.
   */
  offlineSupport?: boolean;
  onOfflineStatusChange?: (status: {
    isOnline: boolean;
    isSyncing: boolean;
    pendingSyncCount: number;
  }) => void;
}
