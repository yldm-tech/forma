import { ChevronDownIcon, XIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "../../lib/cn";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "./dropdown-menu";

interface MultiSelectProps {
  id?: string;
  value: string[];
  options: { value: string; label: string }[];
  onChange: (value: string[]) => void;
  placeholder?: string;
  disabled?: boolean;
}

/** forma2's MultiSelect: the chosen options as removable chips, the rest in a menu. */
export const MultiSelect = ({ id, value, options, onChange, placeholder, disabled }: MultiSelectProps) => {
  const { t } = useTranslation();
  const selected = options.filter((option) => value.includes(option.value));
  const toggle = (optionValue: string) =>
    onChange(value.includes(optionValue) ? value.filter((v) => v !== optionValue) : [...value, optionValue]);
  return (
    <div className="flex flex-col gap-2">
      {selected.length > 0 && (
        <ul className="flex flex-wrap gap-1">
          {selected.map((option) => (
            <li
              key={option.value}
              className="inline-flex items-center gap-1 rounded-md border border-slate-200 bg-slate-100 px-2 py-0.5 text-xs text-slate-700">
              {option.label}
              <button
                type="button"
                disabled={disabled}
                onClick={() => toggle(option.value)}
                aria-label={`${t("common.remove")} ${option.label}`}
                className="text-slate-500 hover:text-slate-800">
                <XIcon className="size-3" />
              </button>
            </li>
          ))}
        </ul>
      )}
      <DropdownMenu>
        <DropdownMenuTrigger asChild disabled={disabled}>
          <button
            id={id}
            type="button"
            className={cn(
              "flex h-10 w-full items-center justify-between rounded-md border border-slate-300 bg-white px-3 py-2 text-left text-sm text-slate-500 focus:ring-2 focus:ring-slate-400 focus:outline-hidden disabled:cursor-not-allowed disabled:opacity-50"
            )}>
            <span className="truncate">{placeholder}</span>
            <ChevronDownIcon className="size-4" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-(--radix-dropdown-menu-trigger-width)">
          {options.map((option) => (
            <DropdownMenuCheckboxItem
              key={option.value}
              checked={value.includes(option.value)}
              onSelect={(e) => {
                e.preventDefault();
                toggle(option.value);
              }}>
              {option.label}
            </DropdownMenuCheckboxItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
};
