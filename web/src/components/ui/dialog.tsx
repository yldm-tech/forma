import { X } from "lucide-react";
import {
  type HTMLAttributes,
  type ReactNode,
  createContext,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../lib/cn";

// forma2's Dialog, on the native <dialog> element: showModal() gives the focus trap, the inert background, Escape and the top layer. Popups opened inside a dialog must render inside it, since everything outside a modal dialog is inert; PortalContainer tells them where.

const DialogContext = createContext<{ titleId: string; descriptionId: string } | null>(null);

/** Where Radix popups (dropdown menus, popovers) portal to: the open dialog, or the body outside one. */
export const PortalContainer = createContext<HTMLElement | null>(null);
export const usePortalContainer = () => useContext(PortalContainer) ?? undefined;

const WIDTHS = {
  default: "sm:w-[90dvw] md:w-[720px]",
  narrow: "sm:w-[512px]",
  wide: "sm:w-[90dvw] md:w-[720px] lg:w-[960px]",
};

interface DialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
  width?: keyof typeof WIDTHS;
  hideCloseButton?: boolean;
  /** Keeps a click on the backdrop and Escape from closing it, for dialogs that hold unsaved input. */
  disableCloseOnOutsideClick?: boolean;
  className?: string;
}

export const Dialog = ({
  open,
  onOpenChange,
  children,
  width = "default",
  hideCloseButton = false,
  disableCloseOnOutsideClick = false,
  className,
}: DialogProps) => {
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

  // A press on the backdrop lands on the <dialog> itself, never on its content; it closes the dialog like Escape does. Keyboard users have Escape and the close button.
  useEffect(() => {
    const dialog = ref.current;
    if (!dialog || disableCloseOnOutsideClick) return;
    const onBackdrop = (event: MouseEvent) => {
      if (event.target === dialog) onOpenChange(false);
    };
    dialog.addEventListener("mousedown", onBackdrop);
    return () => dialog.removeEventListener("mousedown", onBackdrop);
  }, [disableCloseOnOutsideClick, onOpenChange]);

  return (
    <dialog
      ref={(node) => {
        ref.current = node;
        setElement(node);
      }}
      aria-labelledby={titleId}
      aria-describedby={descriptionId}
      onCancel={(e) => {
        e.preventDefault();
        if (!disableCloseOnOutsideClick) onOpenChange(false);
      }}
      onClose={() => {
        if (open) onOpenChange(false);
      }}
      className={cn(
        "m-auto max-h-[90dvh] w-full max-w-[calc(100vw-2rem)] overflow-visible rounded-lg border bg-white p-0 text-left text-slate-900 shadow-lg",
        WIDTHS[width],
        className
      )}>
      {open && (
        <DialogContext.Provider value={{ titleId, descriptionId }}>
          <PortalContainer.Provider value={element}>
            <div className="relative flex max-h-[90dvh] flex-col gap-4 overflow-y-auto p-4">
              {children}
              {!hideCloseButton && (
                <button
                  type="button"
                  onClick={() => onOpenChange(false)}
                  className="absolute top-3 right-3 z-20 rounded-xs bg-transparent opacity-70 transition-opacity hover:opacity-100 focus:ring-2 focus:ring-slate-400 focus:ring-offset-2 focus:outline-hidden">
                  <X className="size-4 text-slate-500" />
                  <span className="sr-only">{t("common.close")}</span>
                </button>
              )}
            </div>
          </PortalContainer.Provider>
        </DialogContext.Provider>
      )}
    </dialog>
  );
};

export const DialogHeader = ({ className, ...props }: HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      "flex shrink-0 flex-col gap-y-1 bg-white pr-6 text-left",
      "[&>svg]:absolute [&>svg]:size-4 [&>svg]:text-primary [&>svg~*]:min-h-4 [&>svg~*]:pl-6",
      className
    )}
    {...props}
  />
);

export const DialogTitle = ({ className, children, ...props }: HTMLAttributes<HTMLHeadingElement>) => {
  const ctx = useContext(DialogContext);
  return (
    <h2
      id={ctx?.titleId}
      className={cn("text-sm leading-none font-medium text-primary", className)}
      {...props}>
      {children}
    </h2>
  );
};

export const DialogDescription = ({ className, ...props }: HTMLAttributes<HTMLParagraphElement>) => {
  const ctx = useContext(DialogContext);
  return <p id={ctx?.descriptionId} className={cn("text-sm text-slate-500", className)} {...props} />;
};

export const DialogBody = ({ className, ...props }: HTMLAttributes<HTMLElement>) => (
  <section className={cn("flex-1 text-sm", className)} {...props} />
);

export const DialogFooter = ({ className, ...props }: HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn("flex shrink-0 flex-col-reverse gap-2 bg-white sm:flex-row sm:justify-end", className)}
    {...props}
  />
);
