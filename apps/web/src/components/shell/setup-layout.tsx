import type { ReactNode } from "react";
import { FormaLogo } from "../ui/logo";

/** forma2's setup layout: one centred card under the product mark. */
export const SetupLayout = ({ children }: { children: ReactNode }) => (
  <main className="flex min-h-dvh w-full items-center justify-center bg-slate-50 p-4">
    <div
      style={{ scrollbarGutter: "stable both-edges" }}
      className="flex max-h-[90dvh] w-full max-w-160 flex-col items-center gap-y-4 overflow-auto rounded-lg border bg-white p-6 text-center shadow-md sm:p-12">
      <div className="size-20 rounded-lg bg-slate-900 p-2">
        <FormaLogo variant="white" className="h-full w-full" />
      </div>
      {children}
    </div>
  </main>
);
