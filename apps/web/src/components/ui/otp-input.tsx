import type { CSSProperties, ChangeEvent, FocusEvent, KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../lib/cn";
import { otpDigits, setOtpDigit } from "../../lib/otp";
import { Input } from "./input";

interface OTPInputProps {
  value: string;
  valueLength: number;
  onChange: (value: string) => void;
  className?: string;
  /** Classes for every digit box, such as the error border of a rejected PIN. */
  inputBoxClassName?: string;
  /** Inline colors for every digit box, such as a survey theme's input colors where the boxes sit on the survey's own background. */
  inputBoxStyle?: CSSProperties;
  disabled?: boolean;
  /** Focuses the first box when it mounts, and when a Dialog holding it opens. */
  autoFocus?: boolean;
}

/** forma2's OTPInput: one box per digit, digits only, pasting a whole code fills every box. */
export const OTPInput = ({
  value,
  valueLength,
  onChange,
  className,
  inputBoxClassName,
  inputBoxStyle,
  disabled,
  autoFocus = false,
}: OTPInputProps) => {
  const { t } = useTranslation();
  const digits = otpDigits(value, valueLength);

  const focusSibling = (target: HTMLElement, direction: "next" | "previous") => {
    const sibling = direction === "next" ? target.nextElementSibling : target.previousElementSibling;
    if (sibling instanceof HTMLInputElement) sibling.focus();
  };

  const handleChange = (e: ChangeEvent<HTMLInputElement>, index: number) => {
    const result = setOtpDigit(value, valueLength, index, e.target.value);
    if (result === null) return;
    onChange(result.value);
    if (result.advance === "next") focusSibling(e.target, "next");
    if (result.advance === "done") e.target.blur();
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    const target = e.currentTarget;
    if (e.key === "ArrowRight" || e.key === "ArrowDown") {
      e.preventDefault();
      focusSibling(target, "next");
      return;
    }
    if (e.key === "ArrowLeft" || e.key === "ArrowUp") {
      e.preventDefault();
      focusSibling(target, "previous");
      return;
    }
    target.setSelectionRange(0, target.value.length);
    if (e.key === "Backspace" && target.value === "") focusSibling(target, "previous");
  };

  const handleFocus = (e: FocusEvent<HTMLInputElement>) => {
    const previous = e.target.previousElementSibling;
    // Focus walks back to the first empty box, so digits are always entered in order.
    if (previous instanceof HTMLInputElement && previous.value === "") {
      previous.focus();
      return;
    }
    e.target.setSelectionRange(0, e.target.value.length);
  };

  return (
    <div className={cn("flex w-full justify-center gap-1.5 sm:gap-2", className)}>
      {digits.map((digit, index) => (
        <Input
          // The boxes are positional; their index is their identity.
          // oxlint-disable-next-line react/no-array-index-key
          key={index}
          type="text"
          inputMode="numeric"
          autoComplete="one-time-code"
          maxLength={valueLength}
          aria-label={t("common.digit_number_of_total", { number: index + 1, total: valueLength })}
          className={cn("h-11 w-full max-w-12 min-w-0 px-0 text-center shadow-xs sm:h-10", inputBoxClassName)}
          style={inputBoxStyle}
          value={digit}
          onChange={(e) => handleChange(e, index)}
          onKeyDown={handleKeyDown}
          onFocus={handleFocus}
          disabled={disabled}
          // autoFocus covers a box mounted into a page or an already open dialog; data-autofocus covers one that is in the dialog when showModal() runs, which moves the focus after React's autoFocus.
          // oxlint-disable-next-line jsx-a11y/no-autofocus -- opt-in, for a step whose only task is typing the code the user was just sent or shown
          autoFocus={autoFocus && index === 0}
          data-autofocus={autoFocus && index === 0 ? "" : undefined}
        />
      ))}
    </div>
  );
};
