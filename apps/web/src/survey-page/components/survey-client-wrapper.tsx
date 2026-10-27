import { getIngestedStorageKeys } from "@forma/types/embedded-data-resolver";
import type { TJsWorkspaceStateSurvey } from "@forma/types/js";
import type { TResponseData } from "@forma/types/responses";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { markRespondedInBrowser } from "../lib/browser-response";
import { buildSurveyDocumentTitle } from "../lib/document-title";
import { readEmbedFrameMode } from "../lib/embed-frame";
import { getHiddenFieldsFromSearchParams, warnOnMissingIngestRows } from "../lib/hidden-fields";
import { getPrefillValue } from "../lib/prefill";
import type { TPublicSurvey, TSurveyProps } from "../lib/state";
import { useAppLocale } from "../lib/use-app-locale";
import { getUserIdFromSearchParams } from "../lib/user-id";
import {
  getElementsFromSurveyBlocks,
  getSurveyLanguageTag,
  getWebAppLocale,
  isRTLLanguage,
} from "../lib/utils";
import { CustomScriptsInjector } from "./custom-scripts-injector";
import { LinkSurveyWrapper } from "./link-survey-wrapper";
import { OfflineAlert } from "./offline-alert";
import { SurveyInline } from "./survey-inline";

type SurveyClientWrapperProps = Omit<TSurveyProps, "survey"> & {
  survey: TPublicSurvey;
  pinAuthToken?: string;
  /** Called once the final answer has reached the server; the PIN gate forgets its kept token then. Must be stable: a new function is a changed prop, which renders the survey again. */
  onFinished?: () => void;
};

/**
 * How tall a card's content may grow before it scrolls inside the card, on the full link page. The rest of the viewport is the page's chrome around the card: the stacked cards peeking above it, the progress bar, the "Powered by" line, the legal footer and the page's margins. The runtime's default (60dvh) suits a widget on someone else's page, not a page of its own. It stays the floor: on a viewport under 480px tall (a phone held sideways) the chrome would leave the card less than that, and there the page scrolls a little instead.
 */
const LINK_PAGE_CARD_MAX_HEIGHT = "max(60dvh, 100dvh - 12rem)";

/** An embedded survey that takes the height of its content (the embed script's frames) never scrolls inside a card: the frame grows to the card instead. A cap on the frame's viewport would also feed back, the frame shrinking to the cap and the cap with it. */
const AUTO_HEIGHT_CARD_MAX_HEIGHT = "none";

/** The cap on a card's content: the viewport on the survey's own page; none in a frame that follows the survey's height; the runtime's own (fullSizeCards) in a frame the survey fills. */
const getCardMaxHeight = (isEmbed: boolean, isAutoHeight: boolean): string | undefined => {
  if (isAutoHeight) return AUTO_HEIGHT_CARD_MAX_HEIGHT;
  return isEmbed ? undefined : LINK_PAGE_CARD_MAX_HEIGHT;
};

// The runtime hands these setters out once it renders; the preview bar's reset button calls them.
let setBlockId = (_: string) => {};
let setResponseData = (_: TResponseData) => {};

// Module-level, so the runtime receives the same functions on every render: a new function is a changed prop, and a changed prop renders the survey again.
const getSetBlockId = (f: (value: string) => void) => {
  setBlockId = f;
};
const getSetResponseData = (f: (value: TResponseData) => void) => {
  setResponseData = f;
};
const getSetIsResponseSendingFinished = (_f: (value: boolean) => void) => {};

/**
 * forma2's SurveyClientWrapper: reads what the URL carries for the survey (prefill, hidden fields, start position, user id, offline mode), keeps the page's language, direction and title in step with the survey, and renders it with the respondent runtime inside the page's chrome.
 */
export const SurveyClientWrapper = ({
  survey,
  workspace,
  styling,
  publicDomain,
  responseCount,
  languageCode,
  isEmbed,
  singleUseId,
  singleUseResponseId,
  contactId,
  canReadUserIdFromUrl,
  recaptchaSiteKey,
  isSpamProtectionEnabled,
  isPreview,
  verifiedEmail,
  imprintUrl,
  privacyUrl,
  termsUrl,
  isFormaCloud,
  pinAuthToken,
  onFinished,
}: SurveyClientWrapperProps) => {
  const searchParams = useMemo(() => new URLSearchParams(globalThis.location.search), []);
  const isAutoHeight = isEmbed && readEmbedFrameMode(globalThis.location.search).isAutoHeight;

  // The survey's active language: the server's, then whatever the in-survey switch picks. The page's own chrome, direction and lang follow it.
  const [currentLanguageCode, setCurrentLanguageCode] = useState(languageCode);
  useAppLocale(getWebAppLocale(currentLanguageCode, survey));

  const skipPrefilled = searchParams.get("skipPrefilled") === "true";
  const offlineSupport = searchParams.get("offlineSupport") === "true";
  // On unless the creator turned it off for a kiosk or a shared device (D-R03); a survey from before the setting existed has it on.
  const persistProgress = survey.isProgressSavingEnabled !== false;
  const userId = canReadUserIdFromUrl ? getUserIdFromSearchParams(searchParams) : undefined;

  // One response per browser (D-FG03): once the final answer is in, this browser shows "already responded" on its next visit (lib/browser-response.ts). A preview is never remembered.
  const remembersResponse = survey.isSingleResponsePerBrowserEnabled === true && !isPreview;
  const surveyIdForMark = survey.id;
  const handleFinished = useCallback(() => {
    if (remembersResponse) markRespondedInBrowser(surveyIdForMark);
    onFinished?.();
  }, [remembersResponse, surveyIdForMark, onFinished]);
  const elements = useMemo(() => getElementsFromSurveyBlocks(survey.blocks), [survey.blocks]);
  const startAt = searchParams.get("startAt");
  const welcomeCardEnabled = survey.welcomeCard.enabled;

  // startAt must name an element, or "start" when the welcome card shows; an invalid one is removed from the URL.
  const isStartAtValid = useMemo(() => {
    if (!startAt) return false;
    if (welcomeCardEnabled && startAt === "start") return true;
    const isValid = elements.some((element) => element.id === startAt);
    if (!isValid) {
      const url = new URL(globalThis.location.href);
      url.searchParams.delete("startAt");
      globalThis.history.replaceState({}, "", url.toString());
    }
    return isValid;
  }, [welcomeCardEnabled, elements, startAt]);

  const prefillValue = useMemo(
    () => getPrefillValue(survey, searchParams, languageCode),
    [survey, searchParams, languageCode]
  );

  // Autofocus only when the survey is the page, not inside another site's frame.
  const [autoFocus] = useState(() => globalThis.self === globalThis.top);

  // Hidden fields fill from the survey's ingested Embedded Data fields, not the legacy fieldIds column.
  const ingestedStorageKeys = useMemo(() => getIngestedStorageKeys(survey), [survey]);
  const hiddenFieldsRecord = useMemo(
    () => getHiddenFieldsFromSearchParams(ingestedStorageKeys, searchParams),
    [ingestedStorageKeys, searchParams]
  );
  const legacyFieldIds = survey.hiddenFields.fieldIds;
  useEffect(() => {
    warnOnMissingIngestRows(ingestedStorageKeys, legacyFieldIds ?? []);
  }, [ingestedStorageKeys, legacyFieldIds]);

  const verifiedEmailField = useMemo<Record<string, string> | null>(
    () => (survey.isVerifyEmailEnabled && verifiedEmail ? { verifiedEmail } : null),
    [survey.isVerifyEmailEnabled, verifiedEmail]
  );
  // The link's token is the respondent's identity for the progress the runtime keeps; only a token the server verified (it then names the address) is passed on.
  const verifyToken =
    survey.isVerifyEmailEnabled && verifiedEmail ? (searchParams.get("verify") ?? undefined) : undefined;
  const surveyHiddenFields = useMemo(
    () => ({ ...hiddenFieldsRecord, ...verifiedEmailField }),
    [hiddenFieldsRecord, verifiedEmailField]
  );

  // The card the respondent is on, labelled by the runtime in the survey's language.
  const [pageLabel, setPageLabel] = useState<string | null>(null);
  const handlePageChange = useCallback((page: { index: number; total: number; label: string }) => {
    setPageLabel(page.label);
  }, []);

  const [offlineStatus, setOfflineStatus] = useState({
    isOnline: true,
    isSyncing: false,
    pendingSyncCount: 0,
  });
  const handleOfflineStatusChange = useCallback(
    (status: { isOnline: boolean; isSyncing: boolean; pendingSyncCount: number }) => {
      setOfflineStatus(status);
    },
    []
  );

  const handleResetSurvey = () => {
    if (survey.welcomeCard.enabled) {
      setBlockId("start");
    } else if (survey.blocks[0]) {
      setBlockId(survey.blocks[0].id);
    }
    setResponseData({});
  };

  // The server already reduced the segment to the runtime's shape (toJsWorkspaceStateSurvey).
  const jsSurvey = survey as unknown as TJsWorkspaceStateSurvey;
  const isCardless = styling.cardArrangement?.linkSurveys === "cardless";
  const hasLogo = !styling.isLogoHidden && !!(styling.logo?.url || workspace.logo?.url);
  const logoDir = useMemo(
    () => (isRTLLanguage(jsSurvey, currentLanguageCode) ? "rtl" : "auto"),
    [currentLanguageCode, jsSurvey]
  );

  // The page's lang and dir follow the survey's active language (WCAG 3.1.1, 1.3.2).
  useEffect(() => {
    const html = document.documentElement;
    const previousLang = html.getAttribute("lang");
    const previousDir = html.getAttribute("dir");
    const tag = getSurveyLanguageTag(jsSurvey, currentLanguageCode);
    if (tag) html.setAttribute("lang", tag);
    html.setAttribute("dir", isRTLLanguage(jsSurvey, currentLanguageCode) ? "rtl" : "ltr");
    return () => {
      if (previousLang === null) html.removeAttribute("lang");
      else html.setAttribute("lang", previousLang);
      if (previousDir === null) html.removeAttribute("dir");
      else html.setAttribute("dir", previousDir);
    };
  }, [currentLanguageCode, jsSurvey]);

  // Every card gets a title saying which it is (WCAG 2.4.2), on top of the title the server rendered.
  const baseTitleRef = useRef<string | null>(null);
  useEffect(() => {
    baseTitleRef.current ??= document.title;
    const baseTitle = baseTitleRef.current;
    return () => {
      document.title = baseTitle;
    };
  }, []);
  useEffect(() => {
    if (baseTitleRef.current === null || !pageLabel) return;
    document.title = buildSurveyDocumentTitle(baseTitleRef.current, pageLabel);
  }, [pageLabel]);

  return (
    <>
      {!isFormaCloud && !isPreview && (
        <CustomScriptsInjector
          workspaceScripts={workspace.customHeadScripts}
          surveyScripts={survey.customHeadScripts}
          scriptsMode={survey.customHeadScriptsMode}
        />
      )}
      <LinkSurveyWrapper
        workspace={workspace}
        workspaceId={survey.workspaceId}
        surveyId={survey.id}
        isWelcomeCardEnabled={survey.welcomeCard.enabled}
        isPreview={isPreview}
        surveyType={survey.type}
        styling={styling}
        handleResetSurvey={handleResetSurvey}
        isEmbed={isEmbed}
        isAutoHeight={isAutoHeight}
        publicDomain={publicDomain}
        isFormaCloud={isFormaCloud}
        imprintUrl={imprintUrl}
        privacyUrl={privacyUrl}
        termsUrl={termsUrl}
        isBrandingEnabled={workspace.linkSurveyBranding}
        dir={logoDir}>
        <SurveyInline
          appUrl={publicDomain}
          workspaceId={survey.workspaceId}
          isPreviewMode={isPreview}
          survey={jsSurvey}
          styling={styling}
          languageCode={languageCode}
          onLanguageChange={setCurrentLanguageCode}
          onPageChange={handlePageChange}
          isBrandingEnabled={workspace.linkSurveyBranding}
          shouldResetQuestionId={false}
          autoFocus={autoFocus}
          prefillResponseData={prefillValue}
          skipPrefilled={skipPrefilled}
          responseCount={responseCount ?? undefined}
          getSetBlockId={getSetBlockId}
          getSetResponseData={getSetResponseData}
          startAtQuestionId={startAt && isStartAtValid ? startAt : undefined}
          fullSizeCards={isEmbed && !isAutoHeight}
          cardMaxHeight={getCardMaxHeight(isEmbed, isAutoHeight)}
          hiddenFieldsRecord={surveyHiddenFields}
          singleUseId={singleUseId}
          singleUseResponseId={singleUseResponseId}
          pinAuthToken={pinAuthToken}
          verifyToken={verifyToken}
          getSetIsResponseSendingFinished={getSetIsResponseSendingFinished}
          contactId={contactId}
          userId={userId}
          recaptchaSiteKey={recaptchaSiteKey}
          isSpamProtectionEnabled={isSpamProtectionEnabled}
          persistProgress={persistProgress}
          offlineSupport={offlineSupport}
          onFinished={handleFinished}
          onOfflineStatusChange={offlineSupport ? handleOfflineStatusChange : undefined}
          showCardlessPreviewLogoSlot={isCardless && hasLogo}
        />
      </LinkSurveyWrapper>
      {offlineSupport && !isEmbed && (
        <OfflineAlert
          isOnline={offlineStatus.isOnline}
          isSyncing={offlineStatus.isSyncing}
          pendingSyncCount={offlineStatus.pendingSyncCount}
        />
      )}
    </>
  );
};
