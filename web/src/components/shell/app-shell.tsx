import type { ReactNode } from "react";
import { PlainChat } from "./plain-chat";

/** forma2's workspace and settings shell: a sidebar, and beside it the top bar over the scrolling page. A banner (the limits notice) floats over both. The Plain support chat is mounted here, as forma2's app layout did, and loads only where the server configured it. */
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
    {banner}
    <PlainChat />
    <div className="flex h-full">
      {sidebar}
      <div id="mainContent" className="flex flex-1 flex-col overflow-hidden bg-slate-50">
        {topBar}
        <main className="flex-1 overflow-y-auto">{children}</main>
      </div>
    </div>
  </div>
);
