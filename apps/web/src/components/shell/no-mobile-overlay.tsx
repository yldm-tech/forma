import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { LinkIcon, LogOutIcon, Maximize2Icon, SmartphoneIcon } from "lucide-react";
import { useEffect, useId, useRef, useState, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { errorMessage } from "../../lib/errors";
import { signOut } from "../../lib/queries/auth";
import { Button } from "../ui/button";

/** Below Tailwind's sm breakpoint, where the overlay covers the app. */
export const PHONE_QUERY = "(max-width: 639px)";

const phoneQuery = () => (typeof window.matchMedia === "function" ? window.matchMedia(PHONE_QUERY) : null);

/** Whether the screen is phone-sized now, following resizes and rotation. */
export const useIsPhone = (): boolean =>
  useSyncExternalStore(
    (onChange) => {
      const query = phoneQuery();
      query?.addEventListener("change", onChange);
      return () => query?.removeEventListener("change", onChange);
    },
    () => phoneQuery()?.matches ?? false,
    () => false
  );

/**
 * forma2's NoMobileOverlay: below the sm breakpoint the signed-in app is covered by a note that it is made for bigger screens, while the surveys it builds work everywhere. It is a modal dialog over the app, which the layout makes inert meanwhile, and it leaves the user two ways on: copy the page's link to open it on a computer, or log out (of an account they may not have meant to use, which it names).
 */
export const NoMobileOverlay = ({ email }: { email: string }) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const titleId = useId();
  const dialog = useRef<HTMLDivElement>(null);
  const [status, setStatus] = useState<string>();
  const [loggingOut, setLoggingOut] = useState(false);

  // The app behind is inert, so focus starts in the dialog.
  useEffect(() => dialog.current?.focus(), []);

  const copyLink = async () => {
    try {
      await navigator.clipboard.writeText(window.location.href);
      setStatus(t("common.copied_to_clipboard"));
    } catch {
      setStatus(t("common.something_went_wrong_please_try_again"));
    }
  };

  const logOut = async () => {
    setLoggingOut(true);
    try {
      await signOut(queryClient, { reason: "user_initiated" });
    } catch (error) {
      setLoggingOut(false);
      setStatus(errorMessage(t, error));
      return;
    }
    await navigate({ href: "/auth/login" });
  };

  return (
    <div
      ref={dialog}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      tabIndex={-1}
      className="fixed inset-0 z-9999 overflow-y-auto bg-slate-50 outline-hidden sm:hidden"
      data-testid="no-mobile-overlay">
      <div className="mx-auto flex min-h-full max-w-xl flex-col items-center justify-center px-4 py-16 text-center">
        <div className="relative size-16">
          <SmartphoneIcon className="size-16 text-slate-500" aria-hidden="true" />
          <Maximize2Icon
            className="absolute top-1/3 left-1/2 size-5 -translate-x-1/2 -translate-y-1/3 text-slate-500"
            aria-hidden="true"
          />
        </div>
        {/* An h2: the page's own h1 stays in the inert app behind. */}
        <h2 id={titleId} className="mt-2 text-2xl font-bold text-zinc-900">
          {t("common.mobile_overlay_title")}
        </h2>
        <p className="mt-2 text-base text-zinc-600">{t("common.mobile_overlay_app_works_best_on_desktop")}</p>
        <p className="mt-2 text-base text-zinc-600">{t("common.mobile_overlay_surveys_look_good")}</p>
        <div className="mt-8 flex w-full max-w-xs flex-col gap-3">
          <p className="text-sm break-all text-zinc-600">{t("common.mobile_overlay_signed_in_as", { email })}</p>
          <Button variant="secondary" onClick={() => void copyLink()}>
            <LinkIcon className="mr-2 size-4" aria-hidden="true" />
            {t("common.mobile_overlay_copy_link")}
          </Button>
          <Button onClick={() => void logOut()} loading={loggingOut}>
            <LogOutIcon className="mr-2 size-4" aria-hidden="true" />
            {t("common.logout")}
          </Button>
          <p role="status" className="min-h-5 text-sm text-zinc-700">
            {status}
          </p>
        </div>
      </div>
    </div>
  );
};
