import * as React from "react";
import { ElementError, getElementErrorAria } from "@/components/general/element-error";
import { ElementHeader } from "@/components/general/element-header";
import { Label } from "@/components/general/label";
import { cn } from "@/lib/utils";

interface SliderProps {
  /** Unique identifier for the element container */
  elementId: string;
  /** The main element or prompt text displayed as the headline */
  headline: string;
  /** Optional descriptive text displayed below the headline */
  description?: string;
  /** Unique identifier for the range input */
  inputId: string;
  /** The smallest value the slider can take */
  min: number;
  /** The largest value the slider can take */
  max: number;
  /** The distance between two neighbouring values */
  step: number;
  /** The answered value; undefined until the respondent has moved the slider */
  value?: number;
  /** Called with the value the respondent moved the slider to */
  onChange: (value: number) => void;
  /** Optional label for the lower end of the scale */
  lowerLabel?: string;
  /** Optional label for the upper end of the scale */
  upperLabel?: string;
  /** Whether the field is required (shows asterisk indicator) */
  required?: boolean;
  /** Custom label for the required indicator */
  requiredLabel?: string;
  /**
   * What the readout shows, and a screen reader announces, before the respondent has moved the slider. The survey runtime supplies a translated one; the English default only covers consumers that render this component outside a survey.
   */
  notAnsweredLabel?: string;
  /** The value as the readout shows it; the number as it is by default */
  formatValue?: (value: number) => string;
  /** Error message to display */
  errorMessage?: string;
  /** Text direction: 'ltr' (left-to-right), 'rtl' (right-to-left), or 'auto' (auto-detect from content) */
  dir?: "ltr" | "rtl" | "auto";
  /** Whether the slider is disabled */
  disabled?: boolean;
  /** Image URL to display above the headline */
  imageUrl?: string;
  /** Video URL to display above the headline */
  videoUrl?: string;
}

/** The decimal places a value of the scale needs: those of the step or of the minimum, whichever has more. */
const getDecimals = (min: number, step: number): number => {
  const decimals = (n: number): number => {
    const text = String(n);
    if (text.includes("e-")) return Number(text.split("e-")[1]);
    return text.split(".")[1]?.length ?? 0;
  };
  return Math.min(10, Math.max(decimals(step), decimals(min)));
};

/**
 * A number picked on a continuous scale. It is a native range input, so arrow keys, Page Up and Down, Home and End and touch work as the platform has them, and an RTL survey runs it from right to left.
 *
 * A slider always shows a position, so an untouched one would look answered: until the respondent moves it (or presses, clicks or taps it where it rests), the thumb rests faded in the middle, the readout says it is not answered, and no value is reported.
 */
function Slider({
  elementId,
  headline,
  description,
  inputId,
  min,
  max,
  step,
  value,
  onChange,
  lowerLabel,
  upperLabel,
  required = false,
  requiredLabel,
  notAnsweredLabel = "Move the slider to answer",
  formatValue = (n) => String(n),
  errorMessage,
  dir = "auto",
  disabled = false,
  imageUrl,
  videoUrl,
}: Readonly<SliderProps>): React.JSX.Element {
  const errorAria = getElementErrorAria(inputId, errorMessage);
  const decimals = getDecimals(min, step);
  const snap = React.useCallback(
    (n: number): number => {
      const positions = Math.round((max - min) / step);
      const position = Math.min(Math.max(Math.round((n - min) / step), 0), positions);
      return Number((min + position * step).toFixed(decimals));
    },
    [min, max, step, decimals]
  );
  const isAnswered = typeof value === "number" && Number.isFinite(value);
  const shown = isAnswered ? snap(value) : snap(min + (max - min) / 2);

  const report = (n: number): void => {
    if (!disabled && Number.isFinite(n)) {
      onChange(snap(n));
    }
  };

  return (
    <div className="w-full space-y-4" id={elementId} dir={dir}>
      <ElementHeader
        headlineId={`${inputId}-headline`}
        headline={headline}
        description={description}
        required={required}
        requiredLabel={requiredLabel}
        htmlFor={inputId}
        imageUrl={imageUrl}
        videoUrl={videoUrl}
      />

      <div className="relative" data-element-input>
        <ElementError errorMessage={errorMessage} dir={dir} id={errorAria.errorId} />
        <div className="w-full px-[2px]">
          {/* The readout repeats what the input announces through aria-valuetext, so it is hidden from assistive technology. */}
          <p
            aria-hidden="true"
            className={cn(
              "font-input text-input-text mb-2 text-center",
              isAnswered ? "text-2xl font-semibold" : "text-sm opacity-70"
            )}>
            {isAnswered ? formatValue(shown) : notAnsweredLabel}
          </p>
          <input
            id={inputId}
            type="range"
            min={min}
            max={max}
            step={step}
            value={shown}
            dir={dir}
            disabled={disabled}
            onChange={(e) => {
              report(Number(e.currentTarget.value));
            }}
            // A press on the thumb where it rests changes nothing and fires no change event, yet the respondent chose that value. A touch tap on a range input fires no click in Chromium, only pointerup and touchend, so pointerup carries the tap and click covers the mouse where pointer events are missing. After a drag the value is already answered, and a swipe that scrolls the page ends in pointercancel, not pointerup.
            onPointerUp={(e) => {
              if (!isAnswered) report(Number(e.currentTarget.value));
            }}
            onClick={(e) => {
              if (!isAnswered) report(Number(e.currentTarget.value));
            }}
            onKeyDown={(e) => {
              if (!isAnswered && (e.key === "Enter" || e.key === " ")) {
                e.preventDefault();
                report(Number(e.currentTarget.value));
              }
            }}
            aria-labelledby={`${inputId}-headline`}
            aria-valuetext={isAnswered ? formatValue(shown) : notAnsweredLabel}
            aria-invalid={errorAria.ariaInvalid}
            aria-describedby={errorAria.ariaDescribedBy}
            className={cn(
              "accent-brand h-6 w-full cursor-pointer",
              !isAnswered && "opacity-50",
              disabled && "cursor-not-allowed opacity-50"
            )}
          />
          <div className="text-input-text mt-1 flex justify-between text-xs opacity-70" aria-hidden="true">
            <span>{formatValue(min)}</span>
            <span>{formatValue(max)}</span>
          </div>
          {(lowerLabel ?? upperLabel) ? (
            <div className="mt-2 flex justify-between gap-8">
              {lowerLabel ? (
                <Label variant="card" className="max-w-[50%] leading-6" dir={dir}>
                  {lowerLabel}
                </Label>
              ) : (
                <span />
              )}
              {upperLabel ? (
                <Label variant="card" className="max-w-[50%] text-right leading-6" dir={dir}>
                  {upperLabel}
                </Label>
              ) : null}
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}

export { Slider };
export type { SliderProps };
