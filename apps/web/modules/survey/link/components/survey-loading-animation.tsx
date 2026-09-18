import { useCallback, useEffect, useRef, useState } from "react";
import { cn } from "@/lib/cn";
import { LoadingSpinner } from "@/modules/ui/components/loading-spinner";
import { Wordmark } from "@/modules/ui/components/wordmark";

/**
 * How long the overlay is held even when everything is ready. Its only job is to stop a flash on a
 * fast connection; anything longer is time a respondent spends looking at a spinner over a question
 * that has already rendered.
 */
const MIN_VISIBLE_MS = 150;

/** Kept in step with `duration-300` on the overlay below. */
const OVERLAY_FADE_MS = 300;

interface SurveyLoadingAnimationProps {
  isWelcomeCardEnabled: boolean;
  isBackgroundLoaded?: boolean;
  isBrandingEnabled: boolean;
}

export const SurveyLoadingAnimation = ({
  isWelcomeCardEnabled,
  isBackgroundLoaded = true,
  isBrandingEnabled,
}: SurveyLoadingAnimationProps) => {
  const [isHidden, setIsHidden] = useState(false);
  const [minTimePassed, setMinTimePassed] = useState(false);
  const [isMediaLoaded, setIsMediaLoaded] = useState(false); // Tracks if all media are fully loaded
  const [isSurveyPackageLoaded, setIsSurveyPackageLoaded] = useState(false); // Tracks if the survey package has been loaded into the DOM
  const isReadyToTransition = isMediaLoaded && minTimePassed && isBackgroundLoaded;
  const overlayRef = useRef<HTMLDivElement>(null);
  const cardId = isWelcomeCardEnabled ? `questionCard--1` : `questionCard-0`;

  // Function to check if all media elements (images and iframes) within the survey card are loaded
  const checkMediaLoaded = useCallback(() => {
    const cardElement = document.getElementById(cardId);
    const images = cardElement ? Array.from(cardElement.getElementsByTagName("img")) : [];

    const allImagesLoaded = images.every((img) => img.complete && img.naturalHeight !== 0);

    if (allImagesLoaded) {
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

    // Set a 3-second timeout to prevent infinite loading
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

  // Hides the overlay once the fade-out has actually finished, rather than after a timer chosen to
  // outlast it. The two were 500 ms and 1000 ms, so the overlay stayed mounted well past the point
  // it had become invisible, and the element it covered was already painted underneath.
  useEffect(() => {
    if (!isReadyToTransition) {
      setIsHidden(false);
      return;
    }

    const overlay = overlayRef.current;
    if (!overlay) {
      setIsHidden(true);
      return;
    }

    const hide = () => {
      setIsHidden(true);
    };
    overlay.addEventListener("transitionend", hide, { once: true });

    // A background-color transition does not fire `transitionend` if the computed value never
    // changes — an already-transparent overlay, or a browser honouring prefers-reduced-motion.
    const fallbackTimer = setTimeout(hide, OVERLAY_FADE_MS + 50);

    return () => {
      overlay.removeEventListener("transitionend", hide);
      clearTimeout(fallbackTimer);
    };
  }, [isReadyToTransition]);

  useEffect(() => {
    // An anti-flash floor, not a staged animation. The comment used to say 1.5 seconds and the timer
    // said 500 ms; both were long enough that a respondent watched a spinner over a question that
    // was already in the DOM — measured at ~300 ms for the question against ~1240 ms for the overlay.
    const minTimeTimer = setTimeout(() => {
      setMinTimePassed(true);
    }, MIN_VISIBLE_MS);

    // Observe the DOM for when the survey package (child elements) is added to the target node
    const observer = new MutationObserver((mutations) => {
      mutations.some((mutation) => {
        if (mutation.addedNodes.length) {
          setIsSurveyPackageLoaded(true);
          observer.disconnect();
          return true;
        }
        return false;
      });
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
          "flex flex-col items-center space-y-4",
          isReadyToTransition ? "animate-surveyExit" : "animate-surveyLoading"
        )}>
        {isBrandingEnabled && (
          <Wordmark
            eyebrow="powered by"
            className={cn("text-3xl transition-all duration-1000 md:text-4xl")}
          />
        )}
        <LoadingSpinner />
      </div>
    </div>
  );
};
