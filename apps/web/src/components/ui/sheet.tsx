import { X } from "lucide-react";
import { type HTMLAttributes, type ReactNode, useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../lib/cn";
import { PortalContainer, dialogEventHandlers } from "./dialog";
import { Toaster } from "./toaster";

// forma2's Sheet: a panel that slides over the right edge of the page (or the left one, for the phone's navigation drawer), on the native <dialog> element like Dialog, so it gets the same focus trap, inert background, Escape and top layer.

interface SheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Read by assistive technology as the panel's name. */
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  className?: string;
  /** The edge the panel sits on: right by default, left for navigation. */
  side?: "left" | "right";
}

export const Sheet = ({
  open,
  onOpenChange,
  title,
  description,
  children,
  className,
  side = "right",
}: SheetProps) => {
  const { t } = useTranslation();
  const ref = useRef<HTMLDialogElement>(null);
  const [element, setElement] = useState<HTMLDialogElement | null>(null);
  const titleId = useId();
  const descriptionId = useId();

  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  // A press on the backdrop lands on the <dialog> itself, never on its content, and closes the panel like Escape does.
  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    const onBackdrop = (event: MouseEvent) => {
      if (event.target === dialog) onOpenChange(false);
    };
    dialog.addEventListener("mousedown", onBackdrop);
    return () => dialog.removeEventListener("mousedown", onBackdrop);
  }, [onOpenChange]);

  return (
    <dialog
      ref={(node) => {
        ref.current = node;
        setElement(node);
      }}
      aria-labelledby={titleId}
      aria-describedby={description ? descriptionId : undefined}
      {...dialogEventHandlers({ open, onOpenChange, disableCloseOnOutsideClick: false })}
      className={cn(
        "m-0 h-dvh max-h-dvh w-full max-w-full border-slate-200 bg-white p-0 text-left text-slate-900 shadow-lg backdrop:bg-black/80",
        side === "right" ? "ml-auto border-l sm:max-w-2xl" : "mr-auto border-r",
        className
      )}>
      {open && (
        <PortalContainer.Provider value={element}>
          <div className="relative flex h-full flex-col gap-4 overflow-y-auto px-5 py-6">
            <div className="flex flex-col space-y-2 pr-8">
              <h2 id={titleId} className="text-lg font-semibold text-slate-900">
                {title}
              </h2>
              {description ? (
                <p id={descriptionId} className="text-sm text-slate-500">
                  {description}
                </p>
              ) : null}
            </div>
            {children}
            <button
              type="button"
              onClick={() => onOpenChange(false)}
              className="focus:ring-ring absolute top-4 right-4 rounded-xs opacity-70 transition-opacity hover:opacity-100 focus:ring-2 focus:ring-offset-2 focus:outline-hidden">
              <X className="size-4" />
              <span className="sr-only">{t("common.close")}</span>
            </button>
          </div>
          <Toaster />
        </PortalContainer.Provider>
      )}
    </dialog>
  );
};

export const SheetSection = ({ className, ...props }: HTMLAttributes<HTMLElement>) => (
  <section className={cn("rounded-lg border border-slate-200 bg-white p-5", className)} {...props} />
);
