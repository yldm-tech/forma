import type { ChangeEvent, FocusEvent, KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../lib/cn";
import { otpDigits, setOtpDigit } from "../../lib/otp";
import { Input } from "./input";

interface OTPInputProps {
  value: string;
  valueLength: number;
  onChange: (value: string) => void;
  className?: string;
  disabled?: boolean;
}

/** forma2's OTPInput: one box per digit, digits only, pasting a whole code fills every box. */
export const OTPInput = ({ value, valueLength, onChange, className, disabled }: OTPInputProps) => {
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
          className="h-11 w-full max-w-12 min-w-0 px-0 text-center shadow-xs sm:h-10"
          value={digit}
          onChange={(e) => handleChange(e, index)}
          onKeyDown={handleKeyDown}
          onFocus={handleFocus}
          disabled={disabled}
        />
      ))}
    </div>
  );
};
