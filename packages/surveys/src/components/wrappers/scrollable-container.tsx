import type { JSX, Ref } from "preact";
import { forwardRef } from "preact/compat";
import { useEffect, useImperativeHandle, useRef, useState } from "preact/hooks";
import { ScrollToBottomButton } from "@/components/buttons/scroll-to-bottom-button";
import { cn } from "@/lib/utils";

/** The height of the top fade (h-4). */
const SCROLL_PADDING_TOP = "1rem";
/** The sticky Back/Next row: a 2.5rem button with py-2 around it, plus a little room. */
const STICKY_FOOTER_SCROLL_PADDING = "4.5rem";

interface ScrollableContainerProps {
  children: JSX.Element;
  fullSizeCards: boolean;
  disableInternalScroll?: boolean;
  /** The host's cap on the scrolling area (SurveyBaseProps.cardMaxHeight), in place of the default share of the viewport. */
  maxHeight?: string;
  /**
   * The content keeps its submit row stuck to the bottom of the scrolling area. The row then is the next action, so the floating scroll-to-bottom button (which sat over answers and over the row itself) and the bottom fade (which faded the row) are left out.
   */
  hasStickyFooter?: boolean;
}

export interface ScrollableContainerHandle {
  scrollToBottom: () => void;
}

export const ScrollableContainer = forwardRef<ScrollableContainerHandle, ScrollableContainerProps>(
  (
    {
      children,
      fullSizeCards = false,
      disableInternalScroll = false,
      maxHeight: hostMaxHeight,
      hasStickyFooter = false,
    }: Readonly<ScrollableContainerProps>,
    ref: Ref<ScrollableContainerHandle>
  ) => {
    const [isAtBottom, setIsAtBottom] = useState(false);
    const [isAtTop, setIsAtTop] = useState(false);
    const containerRef = useRef<HTMLDivElement>(null);
    const isSurveyPreview = Boolean(document.getElementById("survey-preview"));

    const checkScroll = () => {
      if (!containerRef.current) return;
      const { scrollTop, scrollHeight, clientHeight } = containerRef.current;

      // Use a small tolerance to account for zoom-related precision issues
      const tolerance = 1;

      // Check if at bottom with tolerance
      setIsAtBottom(scrollTop + clientHeight >= scrollHeight - tolerance);

      // Check if at top with tolerance
      setIsAtTop(scrollTop <= tolerance);
    };

    const scrollToBottom = () => {
      if (containerRef.current) {
        containerRef.current.scrollTop = containerRef.current.scrollHeight;
      }
    };

    // Expose only the `scrollToBottom` method to parent components via the forwarded ref
    useImperativeHandle(ref, () => ({
      scrollToBottom,
    }));

    useEffect(() => {
      const element = containerRef.current;
      if (!element) return;

      const handleScroll = () => {
        checkScroll();
      };
      element.addEventListener("scroll", handleScroll);

      return () => {
        element.removeEventListener("scroll", handleScroll);
      };
    }, []);

    useEffect(() => {
      checkScroll();
    }, [children]);

    let maxHeight: string | undefined;
    if (disableInternalScroll) {
      maxHeight = undefined;
    } else if (fullSizeCards) {
      maxHeight = "calc(100vh - 6rem)";
    } else if (hostMaxHeight) {
      maxHeight = hostMaxHeight;
    } else if (isSurveyPreview) {
      maxHeight = "42dvh";
    } else {
      maxHeight = "60dvh";
    }

    return (
      <div className="relative">
        {!disableInternalScroll && !isAtTop && (
          <div className="from-survey-bg absolute top-0 right-2 left-0 z-10 h-4 bg-linear-to-b to-transparent" />
        )}
        <div
          ref={containerRef}
          // Marks the card's own scrolling area, so a failed submit can measure how much of a question fits in it.
          data-survey-scroller={disableInternalScroll ? undefined : ""}
          style={{
            maxHeight,
            // Whatever the browser scrolls into view (a control that takes keyboard focus, a question a failed submit points at) lands clear of the top fade and, with a sticky Back/Next row, above that row instead of under it (WCAG 2.4.11).
            scrollPaddingTop: disableInternalScroll ? undefined : SCROLL_PADDING_TOP,
            scrollPaddingBottom:
              !disableInternalScroll && hasStickyFooter ? STICKY_FOOTER_SCROLL_PADDING : undefined,
          }}
          className={cn(disableInternalScroll ? "overflow-visible" : "bg-survey-bg overflow-auto px-4 pt-4")}>
          {children}
        </div>
        {!disableInternalScroll && !isAtBottom && !hasStickyFooter && (
          <>
            <div className="from-survey-bg absolute right-4 bottom-0 left-4 h-4 bg-linear-to-t to-transparent" />
            <ScrollToBottomButton onClick={scrollToBottom} />
          </>
        )}
      </div>
    );
  }
);
