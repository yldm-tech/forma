import { useId, useState } from "react";
import { cn } from "../../lib/cn";

const HEX = /^(?:[0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})$/i;

/** The six-digit form the native color input needs, from any hex color the styling may hold. */
const toSixDigits = (color: string): string => {
  const hex = color.replace("#", "");
  if (hex.length === 3 || hex.length === 4) return `#${[...hex.slice(0, 3)].map((c) => c + c).join("")}`;
  return `#${hex.slice(0, 6)}`;
};

/**
 * forma2's ColorPicker: a hex field and a swatch that opens the system color picker. The field reports a color only once it is a valid 3, 4, 6 or 8 digit hex, and an empty field as "" (no color).
 */
export const ColorPicker = ({
  color,
  onChange,
  containerClass,
  disabled = false,
  placeholder,
  "aria-label": ariaLabel,
}: {
  color: string;
  onChange: (color: string) => void;
  containerClass?: string;
  disabled?: boolean;
  placeholder?: string;
  "aria-label": string;
}) => {
  const id = useId();
  const [text, setText] = useState(color.replace("#", ""));
  // A color set from outside (a swatch, a reset) replaces what is typed.
  const [shownColor, setShownColor] = useState(color);
  if (color !== shownColor) {
    setShownColor(color);
    setText(color.replace("#", ""));
  }

  return (
    <div className={cn(containerClass)}>
      <div className="flex h-10 w-full items-center gap-2 rounded-md border border-slate-300 bg-white px-2 text-sm text-slate-400">
        #
        <input
          id={id}
          aria-label={ariaLabel}
          className="min-w-0 flex-1 border-0 bg-transparent text-slate-500 outline-hidden"
          value={text}
          disabled={disabled}
          placeholder={placeholder}
          maxLength={8}
          onChange={(e) => {
            const next = e.target.value.replaceAll(/[^0-9a-f]/gi, "");
            setText(next);
            const reported = next === "" ? "" : HEX.test(next) ? `#${next}` : undefined;
            if (reported === undefined) return;
            setShownColor(reported);
            onChange(reported);
          }}
        />
        <input
          type="color"
          aria-label={ariaLabel}
          disabled={disabled}
          value={HEX.test(text) ? toSixDigits(text) : "#ffffff"}
          onChange={(e) => onChange(e.target.value)}
          className="size-6 shrink-0 cursor-pointer rounded border border-slate-200 bg-transparent p-0"
        />
      </div>
    </div>
  );
};
