import {
  type HTMLAttributes,
  type ReactNode,
  useCallback,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { cn } from "../../../lib/cn";

/** A scrolling list that fades out at its bottom edge while more of it is below, so a list cut off by its height reads as one that scrolls (macOS hides scrollbars until used). */
export const ScrollFadeList = ({
  className,
  children,
  ...props
}: HTMLAttributes<HTMLDivElement> & { children: ReactNode }) => {
  const ref = useRef<HTMLDivElement>(null);
  const [hasMoreBelow, setHasMoreBelow] = useState(false);
  const update = useCallback(() => {
    const element = ref.current;
    if (element) setHasMoreBelow(element.scrollTop + element.clientHeight < element.scrollHeight - 1);
  }, []);
  // Measured again whenever the list's content changes (a tab or a search) or its box is resized.
  useLayoutEffect(() => {
    update();
    const element = ref.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(update);
    observer.observe(element);
    return () => observer.disconnect();
  }, [update, children]);
  return (
    <div
      ref={ref}
      onScroll={update}
      data-has-more-below={hasMoreBelow || undefined}
      className={cn(
        className,
        hasMoreBelow && "[mask-image:linear-gradient(to_bottom,black_calc(100%_-_3rem),transparent)]"
      )}
      {...props}>
      {children}
    </div>
  );
};

/** A titled group of an add-filter list's All tab, so each kind of filter is found under its own heading. */
export const FilterSection = ({ label, children }: { label: string; children: ReactNode }) => {
  const headingId = useId();
  return (
    <div role="group" aria-labelledby={headingId} className="flex flex-col gap-1 pb-1">
      <p id={headingId} className="px-2 pt-2 text-xs font-semibold text-slate-500">
        {label}
      </p>
      {children}
    </div>
  );
};
