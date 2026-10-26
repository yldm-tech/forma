import { EyeIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useIsPhoneReadyPage } from "../../lib/phone";
import { useIsPhone } from "./no-mobile-overlay";
import { PlainChat } from "./plain-chat";
import { UnsavedChangesGuard } from "./unsaved-changes";

/** The id of the shell's <main>, the skip link's target. */
export const MAIN_CONTENT_ID = "main-content";

/** The first stop of the keyboard: hidden until focused, it moves the focus past the sidebar and the top bar to the page. The focus is moved by hand, since following the #hash would make the router navigate. */
const SkipToContent = () => {
  const { t } = useTranslation();
  return (
    <a
      href={`#${MAIN_CONTENT_ID}`}
      onClick={(event) => {
        event.preventDefault();
        document.getElementById(MAIN_CONTENT_ID)?.focus();
      }}
      className="focus-visible:ring-ring sr-only rounded-md bg-white px-3 py-2 text-sm font-medium text-slate-900 shadow-md focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:z-[200] focus-visible:ring-2 focus-visible:outline-hidden">
      {t("common.skip_to_content")}
    </a>
  );
};

/** On a phone-sized screen, over a page that opens there (PHONE_READY), the note that the page is read-only and that changing anything needs a bigger screen. It is left out for a user whose access to the workspace is read-only anyway, whom a bigger screen would not let change anything either. */
const PhoneViewOnlyNotice = ({ isWorkspaceReadOnly }: { isWorkspaceReadOnly: boolean }) => {
  const { t } = useTranslation();
  const isPhone = useIsPhone();
  const isPhoneReadyPage = useIsPhoneReadyPage();
  if (!isPhone || !isPhoneReadyPage || isWorkspaceReadOnly) return null;
  return (
    <p className="flex items-center gap-2 border-y border-slate-200 bg-slate-100 px-3 py-2 text-xs text-slate-600">
      <EyeIcon className="size-4 shrink-0" strokeWidth={1.5} aria-hidden="true" />
      {t("common.mobile_view_only_notice")}
    </p>
  );
};

/** forma2's workspace and settings shell: a sidebar, and beside it the top bar over the scrolling page. A banner (the limits notice) floats over both. Below the sm breakpoint the sidebar is hidden and the top bar's menu opens the navigation in a drawer instead, and a page that opens on a phone says under the top bar that it is read-only there, unless the user's access to the workspace is read-only anyway. The shell is as tall as the visible viewport (dvh), so a phone's browser bars do not hide the foot of the page. The Plain support chat is mounted here, as forma2's app layout did, and loads only where the server configured it. Every page in it can hold back a navigation while a form has unsaved edits (UnsavedChangesGuard). */
export const AppShell = ({
  sidebar,
  topBar,
  banner,
  isWorkspaceReadOnly = false,
  children,
}: {
  sidebar: ReactNode;
  topBar: ReactNode;
  banner?: ReactNode;
  /** The user's access to the workspace on screen is read-only on any screen (a member whom no team grants write). */
  isWorkspaceReadOnly?: boolean;
  children: ReactNode;
}) => (
  <div className="flex h-dvh min-h-dvh flex-col overflow-hidden">
    <SkipToContent />
    {banner}
    <PlainChat />
    <div className="flex h-full">
      <div className="hidden sm:flex">{sidebar}</div>
      <div id="mainContent" className="flex flex-1 flex-col overflow-hidden bg-slate-50">
        {topBar}
        <PhoneViewOnlyNotice isWorkspaceReadOnly={isWorkspaceReadOnly} />
        <main id={MAIN_CONTENT_ID} tabIndex={-1} className="flex-1 overflow-y-auto outline-hidden">
          <UnsavedChangesGuard>{children}</UnsavedChangesGuard>
        </main>
      </div>
    </div>
  </div>
);
