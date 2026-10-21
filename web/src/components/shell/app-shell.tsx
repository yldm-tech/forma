import type { ReactNode } from "react";

/** forma2's workspace and settings shell: a sidebar, and beside it the top bar over the scrolling page. */
export const AppShell = ({
  sidebar,
  topBar,
  children,
}: {
  sidebar: ReactNode;
  topBar: ReactNode;
  children: ReactNode;
}) => (
  <div className="flex h-screen min-h-screen flex-col overflow-hidden">
    <div className="flex h-full">
      {sidebar}
      <div id="mainContent" className="flex flex-1 flex-col overflow-hidden bg-slate-50">
        {topBar}
        <main className="flex-1 overflow-y-auto">{children}</main>
      </div>
    </div>
  </div>
);
