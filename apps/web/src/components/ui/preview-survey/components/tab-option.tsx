import { ReactNode } from "react";

interface TabOptionProps {
  active: boolean;
  icon: ReactNode;
  /** The option's name; the button shows only its icon. */
  label: string;
  onClick: () => void;
}

export const TabOption = ({ active, icon, label, onClick }: TabOptionProps) => {
  return (
    <button
      type="button"
      aria-label={label}
      aria-pressed={active}
      className={`${active ? "rounded-full bg-slate-200" : ""} cursor-pointer`}
      onClick={onClick}>
      {icon}
    </button>
  );
};
