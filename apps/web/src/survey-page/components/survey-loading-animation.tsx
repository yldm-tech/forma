import { useCallback, useEffect, useRef, useState } from "react";
import { cn } from "../../lib/cn";
import { LoadingSpinner } from "../../components/ui/loading-spinner";
import { Wordmark } from "../../components/ui/logo";
import { fadeOutFirstPaint, hasFirstPaint } from "../lib/first-paint";

/**
 * How long the overlay is held even when everything is ready. Its only job is to stop a flash on a fast connection; anything longer is time a respondent spends looking at a spinner over a question that has already rendered.
 */
const MIN_VISIBLE_MS = 150;

/** Kept in step with `duration-300` on the overlay below. */
const OVERLAY_FADE_MS = 300;

interface SurveyLoadingAnimationProps {
  isWelcomeCardEnabled: boolean;
  isBackgroundLoaded?: boolean;
  isBrandingEnabled: boolean;
}

/** forma2's SurveyLoadingAnimation: a white overlay over the survey until the runtime has rendered its first card and that card's media has loaded. Where the server painted the survey's title over the page (lib/first-paint.ts), that paint is the overlay respondents see: this one keeps only its white, under it, and fades the paint out with itself. */
export const SurveyLoadingAnimation = ({
  isWelcomeCardEnabled,
  isBackgroundLoaded = true,
  isBrandingEnabled,
}: SurveyLoadingAnimationProps) => {
  const [isHidden, setIsHidden] = useState(false);
  const [minTimePassed, setMinTimePassed] = useState(false);
  const [isMediaLoaded, setIsMediaLoaded] = useState(false);
  const [isSurveyPackageLoaded, setIsSurveyPackageLoaded] = useState(false);
  const isReadyToTransition = isMediaLoaded && minTimePassed && isBackgroundLoaded;
  const overlayRef = useRef<HTMLDivElement>(null);
  const cardId = isWelcomeCardEnabled ? `questionCard--1` : `questionCard-0`;
  // Read on mount: once faded, the paint is gone from the page.
  const [isUnderFirstPaint] = useState(() => hasFirstPaint());

  // Every image in the first card must have loaded.
  const checkMediaLoaded = useCallback(() => {
    const cardElement = document.getElementById(cardId);
    const images = cardElement ? Array.from(cardElement.getElementsByTagName("img")) : [];
    if (images.every((img) => img.complete && img.naturalHeight !== 0)) {
      setIsMediaLoaded(true);
    }
  }, [cardId]);

  useEffect(() => {
    if (!isSurveyPackageLoaded) return;

    checkMediaLoaded();

    const mediaElements = document.querySelectorAll(`#${cardId} img, #${cardId} iframe`);
    const handleLoad = () => {
      checkMediaLoaded();
    };
    const handleError = () => {
      setIsMediaLoaded(true);
    };
    mediaElements.forEach((element) => {
      element.addEventListener("load", handleLoad);
      element.addEventListener("error", handleError);
    });

    // Media that never loads must not hold the survey hostage.
    const timeoutId = setTimeout(() => {
      setIsMediaLoaded(true);
    }, 3000);

    return () => {
      mediaElements.forEach((element) => {
        element.removeEventListener("load", handleLoad);
        element.removeEventListener("error", handleError);
      });
      clearTimeout(timeoutId);
    };
  }, [isSurveyPackageLoaded, checkMediaLoaded, cardId]);

  // Hides the overlay once its fade-out has finished.
  useEffect(() => {
    if (!isReadyToTransition) {
      setIsHidden(false);
      return;
    }
    fadeOutFirstPaint(OVERLAY_FADE_MS);
    const overlay = overlayRef.current;
    if (!overlay) {
      setIsHidden(true);
      return;
    }
    const hide = () => {
      setIsHidden(true);
    };
    overlay.addEventListener("transitionend", hide, { once: true });
    // A background-color transition does not fire `transitionend` if the computed value never changes, as under prefers-reduced-motion.
    const fallbackTimer = setTimeout(hide, OVERLAY_FADE_MS + 50);
    return () => {
      overlay.removeEventListener("transitionend", hide);
      clearTimeout(fallbackTimer);
    };
  }, [isReadyToTransition]);

  useEffect(() => {
    const minTimeTimer = setTimeout(() => {
      setMinTimePassed(true);
    }, MIN_VISIBLE_MS);

    // The runtime renders into #forma-survey-container; its first child means the bundle has run.
    const observer = new MutationObserver((mutations) => {
      if (mutations.some((mutation) => mutation.addedNodes.length > 0)) {
        setIsSurveyPackageLoaded(true);
        observer.disconnect();
      }
    });
    const targetNode = document.getElementById("forma-survey-container");
    if (targetNode) {
      observer.observe(targetNode, { childList: true });
    }

    return () => {
      observer.disconnect();
      clearTimeout(minTimeTimer);
    };
  }, []);

  return (
    <div
      ref={overlayRef}
      className={cn(
        "absolute inset-0 z-5000 flex items-center justify-center transition-colors duration-300",
        isReadyToTransition ? "bg-transparent" : "bg-white",
        isHidden && "hidden"
      )}>
      <div
        className={cn(
          "flex flex-col items-center space-y-4 transition-opacity duration-300",
          isReadyToTransition ? "opacity-0" : "opacity-100"
        )}>
        {isBrandingEnabled && !isUnderFirstPaint && (
          <div className="flex flex-col items-center gap-1">
            <span className="text-xs tracking-wide text-slate-500">powered by</span>
            <Wordmark className="text-3xl md:text-4xl" />
          </div>
        )}
        {!isUnderFirstPaint && <LoadingSpinner />}
      </div>
    </div>
  );
};
