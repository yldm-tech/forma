import { useEffect, useRef, useState } from "react";

/** forma2's ScrollToTop: a button that appears once the page scrolled 300 px down and scrolls its container back up. The container is the scrolling element around the button, the app shell's main area. */
export const ScrollToTop = () => {
  const anchorRef = useRef<HTMLSpanElement>(null);
  const [container, setContainer] = useState<HTMLElement | null>(null);
  const [showButton, setShowButton] = useState(false);

  useEffect(() => {
    setContainer(anchorRef.current?.closest("main") ?? null);
  }, []);

  useEffect(() => {
    if (!container) return;
    const handleScroll = () => setShowButton(container.scrollTop > 300);
    container.addEventListener("scroll", handleScroll);
    return () => container.removeEventListener("scroll", handleScroll);
  }, [container]);

  return (
    <span ref={anchorRef}>
      <button
        type="button"
        tabIndex={showButton ? 0 : -1}
        onClick={() => container?.scrollTo({ top: 0, behavior: "smooth" })}
        className={`fixed right-4 bottom-4 z-1 flex h-10 w-10 justify-center rounded-md bg-slate-500 p-2 text-white transition-opacity ${
          showButton ? "opacity-80" : "pointer-events-none opacity-0"
        }`}>
        ↑
      </button>
    </span>
  );
};
