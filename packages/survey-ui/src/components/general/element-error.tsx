import { AlertCircle } from "lucide-react";
import * as React from "react";
import { cn } from "@/lib/utils";

interface ElementErrorProps {
  /** Error message to display */
  errorMessage?: string;
  /** Text direction: 'ltr' (left-to-right), 'rtl' (right-to-left), or 'auto' (auto-detect from content) */
  dir?: "ltr" | "rtl" | "auto";
  /**
   * Id placed on the message so the invalid control can point at it with aria-describedby.
   * Call sites use the `${inputId}-error` convention.
   */
  id: string;
  /**
   * Announce the message as it appears. Off by default: a failed submit moves focus to the first invalid control, which reads this message through aria-describedby, and the block announces one summary; ten live regions changing at once queued ten identical phrases. Only an error that appears without a submit (a file that failed to upload) needs announcing on its own.
   */
  live?: boolean;
}

interface ElementErrorAria {
  errorId: string;
  ariaInvalid: boolean;
  ariaDescribedBy: string | undefined;
}

const getElementErrorAria = (inputId: string, errorMessage?: string): ElementErrorAria => {
  const errorId = `${inputId}-error`;
  return {
    errorId,
    ariaInvalid: Boolean(errorMessage),
    ariaDescribedBy: errorMessage ? errorId : undefined,
  };
};

function ElementError({
  errorMessage,
  dir = "auto",
  id,
  live = false,
}: Readonly<ElementErrorProps>): React.JSX.Element {
  return (
    <>
      {/* Error indicator bar: decorative, so it stays outside the live region (a live region must
          announce only the message) and renders only while there is an error. */}
      {errorMessage ? (
        <div
          className={cn(
            "bg-destructive absolute top-0 bottom-0 w-[4px]",
            dir === "rtl" ? "right-[-10px]" : "left-[-10px]"
          )}
        />
      ) : null}
      {/* Error message - shown at top. Always mounted, even with no message: a live region only
          reliably announces content inserted into a region that already existed, and an
          aria-describedby target must never be a missing id. While empty it has no children and no
          classes, so it paints nothing and takes no space. */}
      <div
        id={id}
        aria-live={live ? "polite" : undefined}
        aria-atomic={live ? "true" : undefined}
        className={errorMessage ? "text-destructive mb-2 flex items-center gap-1 text-sm" : undefined}
        dir={dir}>
        {errorMessage ? (
          <>
            <AlertCircle className="size-4" />
            <span>{errorMessage}</span>
          </>
        ) : null}
      </div>
    </>
  );
}

export { ElementError };
export { getElementErrorAria };
export type { ElementErrorAria, ElementErrorProps };
