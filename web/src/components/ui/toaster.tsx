import { AlertCircleIcon, CheckCircle2Icon, Loader2, X } from "lucide-react";
import { useEffect, useId, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../lib/cn";
import { toast, toasterHosts } from "../../lib/toast";

const ICONS = {
  success: <CheckCircle2Icon className="size-4 shrink-0 text-green-600" />,
  error: <AlertCircleIcon className="size-4 shrink-0 text-red-600" />,
  loading: <Loader2 className="size-4 shrink-0 animate-spin text-slate-500" />,
};

/** Renders the toast store: errors interrupt assistive technology, everything else waits its turn. The page mounts one and every open modal dialog another (see toasterHosts); only the newest draws, so a toast raised while a dialog is open shows above its backdrop. */
export const Toaster = () => {
  const { t } = useTranslation();
  const id = useId();
  useEffect(() => toasterHosts.register(id), [id]);
  const host = useSyncExternalStore(toasterHosts.subscribe, toasterHosts.getSnapshot);
  const toasts = useSyncExternalStore(toast.subscribe, toast.getSnapshot);
  if (host !== id) return null;
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-4 z-[100] flex flex-col items-center gap-2 px-4">
      {toasts.map((item) => (
        <div
          key={item.id}
          role={item.kind === "error" ? "alert" : "status"}
          className={cn(
            "pointer-events-auto flex max-w-md items-start gap-2 rounded-lg border border-slate-200 bg-white px-4 py-3 text-sm whitespace-pre-line text-slate-800 shadow-lg"
          )}>
          {ICONS[item.kind]}
          <span className="flex-1">{item.message}</span>
          {item.kind !== "loading" && (
            <button
              type="button"
              onClick={() => toast.dismiss(item.id)}
              className="text-slate-400 hover:text-slate-600"
              aria-label={t("common.close")}>
              <X className="size-4" />
            </button>
          )}
        </div>
      ))}
    </div>
  );
};
