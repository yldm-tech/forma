import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
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

/** forma2's workspace and settings shell: a sidebar, and beside it the top bar over the scrolling page. A banner (the limits notice) floats over both. The Plain support chat is mounted here, as forma2's app layout did, and loads only where the server configured it. Every page in it can hold back a navigation while a form has unsaved edits (UnsavedChangesGuard). */
export const AppShell = ({
  sidebar,
  topBar,
  banner,
  children,
}: {
  sidebar: ReactNode;
  topBar: ReactNode;
  banner?: ReactNode;
  children: ReactNode;
}) => (
  <div className="flex h-screen min-h-screen flex-col overflow-hidden">
    <SkipToContent />
    {banner}
    <PlainChat />
    <div className="flex h-full">
      {sidebar}
      <div id="mainContent" className="flex flex-1 flex-col overflow-hidden bg-slate-50">
        {topBar}
        <main id={MAIN_CONTENT_ID} tabIndex={-1} className="flex-1 overflow-y-auto outline-hidden">
          <UnsavedChangesGuard>{children}</UnsavedChangesGuard>
        </main>
      </div>
    </div>
  </div>
);
