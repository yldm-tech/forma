import { EyeIcon, EyeOff } from "lucide-react";
import { type InputHTMLAttributes, forwardRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../lib/cn";

export interface PasswordInputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "type"> {
  containerClassName?: string;
}

export const PasswordInput = forwardRef<HTMLInputElement, PasswordInputProps>(
  ({ className, containerClassName, ...rest }, ref) => {
    const [showPassword, setShowPassword] = useState(false);
    const { t } = useTranslation();
    return (
      <div className={cn("relative", containerClassName)}>
        <input
          ref={ref}
          type={showPassword ? "text" : "password"}
          className={cn(
            "flex h-10 w-full rounded-md border border-slate-300 bg-transparent px-3 py-2 pr-11 text-base text-slate-800 placeholder:text-slate-500 focus:outline-hidden disabled:cursor-not-allowed disabled:opacity-50 sm:text-sm",
            className
          )}
          {...rest}
        />
        <button
          type="button"
          className="focus-visible:ring-brand-dark absolute top-1/2 right-1 flex size-9 -translate-y-1/2 items-center justify-center rounded-md text-slate-400 transition-colors hover:text-slate-600 focus-visible:ring-2 focus-visible:outline-hidden"
          aria-label={showPassword ? t("common.hide_password") : t("common.show_password")}
          aria-pressed={showPassword}
          onClick={() => setShowPassword((shown) => !shown)}>
          {showPassword ? <EyeOff className="size-5" /> : <EyeIcon className="size-5" />}
        </button>
      </div>
    );
  }
);
PasswordInput.displayName = "PasswordInput";
