import * as React from "react";
import { Calendar } from "@/components/general/calendar";
import { ElementError, getElementErrorAria } from "@/components/general/element-error";
import { ElementHeader } from "@/components/general/element-header";
import { Input } from "@/components/general/input";
import { Label } from "@/components/general/label";
import { getDateFnsLocale } from "@/lib/locale";

interface DateElementProps {
  /** Unique identifier for the element container */
  elementId: string;
  /** The main element or prompt text displayed as the headline */
  headline: string;
  /** Optional descriptive text displayed below the headline */
  description?: string;
  /** Unique identifier for the date input */
  inputId: string;
  /** Current value: the date in ISO format (YYYY-MM-DD), followed by the time (THH:mm) when includeTime is on */
  value?: string;
  /** Callback function called when the date value changes */
  onChange: (value: string) => void;
  /** Whether the field is required (shows asterisk indicator) */
  required?: boolean;
  /** Custom label for the required indicator */
  requiredLabel?: string;
  /** Minimum date allowed (ISO format: YYYY-MM-DD) */
  minDate?: string;
  /** Maximum date allowed (ISO format: YYYY-MM-DD) */
  maxDate?: string;
  /** Error message to display */
  errorMessage?: string;
  /** Text direction: 'ltr' (left-to-right), 'rtl' (right-to-left), or 'auto' (auto-detect from content) */
  dir?: "ltr" | "rtl" | "auto";
  /** Whether the date input is disabled */
  disabled?: boolean;
  /** Locale code for date formatting (e.g., "en-US", "de-DE", "fr-FR"). Defaults to browser locale or "en-US" */
  locale?: string;
  /** Image URL to display above the headline */
  imageUrl?: string;
  /** Video URL to display above the headline */
  videoUrl?: string;
  /** Whether the respondent also picks a time of day; the value is then YYYY-MM-DDTHH:mm once both are picked */
  includeTime?: boolean;
  /** Label of the time input. The survey runtime supplies a translated one; the English default only covers consumers that render this component outside a survey. */
  timeLabel?: string;
}

/** The time of day (HH:mm) a value carries, "" for none. */
const getTimeOfDay = (value: string | undefined): string => {
  const time = value?.split("T")[1] ?? "";
  return /^\d{2}:\d{2}$/.test(time) ? time : "";
};

function DateElement({
  elementId,
  headline,
  description,
  inputId,
  value,
  onChange,
  required = false,
  requiredLabel,
  minDate,
  maxDate,
  dir = "auto",
  disabled = false,
  locale = "en-US",
  errorMessage,
  imageUrl,
  videoUrl,
  includeTime = false,
  timeLabel = "Time",
}: Readonly<DateElementProps>): React.JSX.Element {
  const errorAria = getElementErrorAria(inputId, errorMessage);

  // The time picked so far. It is kept apart from the value: a time picked before the day has nowhere to go in a YYYY-MM-DD value, and must not be lost when the day follows.
  const [time, setTime] = React.useState<string>(() => getTimeOfDay(value));
  // A value given from outside (going back to the question, a prefill) brings its own time; adjusted while rendering rather than in an effect, so the input never shows the old time for a frame.
  const [timeSource, setTimeSource] = React.useState(value);
  if (value !== timeSource) {
    setTimeSource(value);
    const fromValue = getTimeOfDay(value);
    if (fromValue) setTime(fromValue);
  }

  /** The value for a day and the time picked: the day alone until a time is picked, nothing without a day. */
  const compose = (day: string, timeOfDay: string): string => {
    if (!day) return "";
    return includeTime && timeOfDay ? `${day}T${timeOfDay}` : day;
  };

  // Initialize date from value string, parsing as local time to avoid timezone issues
  const [date, setDate] = React.useState<Date | undefined>(() => {
    if (!value) return undefined;
    // Parse YYYY-MM-DD format as local date (not UTC)
    const [year, month, day] = value.slice(0, 10).split("-").map(Number);
    return new Date(year, month - 1, day);
  });

  // Sync date state when value prop changes
  React.useEffect(() => {
    if (value) {
      // Parse YYYY-MM-DD format as local date (not UTC)
      const [year, month, day] = value.slice(0, 10).split("-").map(Number);
      const newDate = new Date(year, month - 1, day);
      setDate((prevDate) => {
        // Only update if the date actually changed to avoid unnecessary re-renders
        if (newDate.getTime() !== prevDate?.getTime()) {
          return newDate;
        }
        return prevDate;
      });
    } else {
      setDate(undefined);
    }
  }, [value]);

  // Convert Date to ISO string (YYYY-MM-DD) when date changes
  const handleDateSelect = (selectedDate: Date | undefined): void => {
    setDate(selectedDate);
    if (selectedDate) {
      // Convert to ISO format (YYYY-MM-DD) using local time to avoid timezone issues
      const year = String(selectedDate.getFullYear());
      const month = String(selectedDate.getMonth() + 1).padStart(2, "0");
      const day = String(selectedDate.getDate()).padStart(2, "0");
      const isoString = `${year}-${month}-${day}`;
      onChange(compose(isoString, time));
    } else {
      onChange("");
    }
  };

  const selectedDay = value ? value.slice(0, 10) : "";

  const handleTimeChange = (timeOfDay: string): void => {
    setTime(timeOfDay);
    if (selectedDay) {
      onChange(compose(selectedDay, timeOfDay));
    }
  };

  // Get locale for date formatting
  const dateLocale = React.useMemo(() => {
    return locale ? getDateFnsLocale(locale) : undefined;
  }, [locale]);

  const startMonth = React.useMemo(() => {
    if (!minDate) return undefined;
    try {
      const [year, month, day] = minDate.split("-").map(Number);
      return new Date(year, month - 1, day);
    } catch {
      return undefined;
    }
  }, [minDate]);

  const endMonth = React.useMemo(() => {
    if (!maxDate) return undefined;
    try {
      const [year, month, day] = maxDate.split("-").map(Number);
      return new Date(year, month - 1, day);
    } catch {
      return undefined;
    }
  }, [maxDate]);

  // Create disabled function for date restrictions
  const isDateDisabled = React.useCallback(
    (dateToCheck: Date): boolean => {
      if (disabled) return true;

      const checkAtMidnight = new Date(
        dateToCheck.getFullYear(),
        dateToCheck.getMonth(),
        dateToCheck.getDate()
      );

      if (startMonth) {
        const minAtMidnight = new Date(startMonth.getFullYear(), startMonth.getMonth(), startMonth.getDate());
        if (checkAtMidnight < minAtMidnight) return true;
      }

      if (endMonth) {
        const maxAtMidnight = new Date(endMonth.getFullYear(), endMonth.getMonth(), endMonth.getDate());
        if (checkAtMidnight > maxAtMidnight) return true;
      }
      return false;
    },
    [disabled, endMonth, startMonth]
  );

  return (
    <div className="w-full space-y-4" id={elementId} dir={dir}>
      {/* Headline */}
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
        {/* Calendar - Always visible. The value is picked from a grid of day buttons, so there is
            no single native control to flag: a native <fieldset> wraps them and carries the invalid
            state. It is named by the headline via aria-labelledby rather than a <legend>, so the
            headline's media/required badge are not nested in invalid block content;
            m-0/p-0/border-0/min-w-0 reset the fieldset UA defaults so it lays out like a plain div.

            The fieldset's role is still "group", which ARIA 1.2 does not give aria-invalid (it was
            global in 1.1, and the native element only keeps the lint rule quiet — it does not make
            the attribute supported). The day cells belong to the Calendar's own grid, so there is
            no accurate role to swap in here. aria-invalid stays as a best-effort hook; the live
            region above plus the focus move are what actually announce the failure. */}
        <fieldset
          className="m-0 w-full min-w-0 border-0 p-0"
          aria-labelledby={`${inputId}-headline`}
          aria-invalid={errorAria.ariaInvalid}
          aria-describedby={errorAria.ariaDescribedBy}>
          <Calendar
            mode="single"
            selected={date}
            defaultMonth={date}
            captionLayout="dropdown"
            startMonth={startMonth}
            endMonth={endMonth}
            disabled={isDateDisabled}
            onSelect={handleDateSelect}
            locale={dateLocale}
            required={required}
            className="rounded-input border-input-border bg-input-bg text-input-text shadow-input mx-auto h-[stretch] w-full max-w-[25rem] border"
          />
          {includeTime ? (
            <div className="mx-auto mt-3 flex w-full max-w-[25rem] items-center gap-3">
              <Label htmlFor={`${inputId}-time`} variant="default">
                {timeLabel}
              </Label>
              <Input
                id={`${inputId}-time`}
                type="time"
                value={time}
                dir={dir}
                disabled={disabled}
                aria-invalid={errorAria.ariaInvalid}
                aria-describedby={errorAria.ariaDescribedBy}
                onChange={(e) => {
                  handleTimeChange(e.currentTarget.value);
                }}
                className="w-auto"
              />
            </div>
          ) : null}
        </fieldset>
      </div>
    </div>
  );
}

export { DateElement };
export type { DateElementProps };
