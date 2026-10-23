import { useTranslation } from "react-i18next";
import { passwordChecks } from "../validation";

/** The checklist under a new password. aria-live announces a rule flipping as the user types. */
export function PasswordChecks({ password }: { password: string }) {
  const { t } = useTranslation();
  return (
    <div className="my-2 text-left text-sm text-slate-700">
      <ul aria-label={t("auth.signup.password_requirements")} aria-live="polite">
        {passwordChecks(password).map((check) => (
          <li key={check.key} className="flex items-start gap-2">
            {check.passed ? (
              <svg
                aria-hidden="true"
                viewBox="0 0 24 24"
                className="size-5 shrink-0"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round">
                <path d="M20 6 9 17l-5-5" />
              </svg>
            ) : (
              <span className="flex size-5 shrink-0 items-center justify-center" aria-hidden="true">
                <i className="inline-block size-2 rounded-full bg-slate-700" />
              </span>
            )}
            <span>{t(check.key)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
