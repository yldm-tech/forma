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
import { Toaster } from "./toaster";

// forma2's Dialog, on the native <dialog> element: showModal() gives the focus trap, the inert background, Escape and the top layer. Popups opened inside a dialog must render inside it, since everything outside a modal dialog is inert; PortalContainer tells them where, and the dialog draws the toasts itself while it is open.

const DialogContext = createContext<{ titleId: string; descriptionId: string } | null>(null);

/** Where Radix popups (dropdown menus, popovers) portal to: the open dialog, or the body outside one. */
export const PortalContainer = createContext<HTMLElement | null>(null);
export const usePortalContainer = () => useContext(PortalContainer) ?? undefined;

const WIDTHS = {
  default: "sm:w-[90dvw] md:w-[720px]",
  narrow: "sm:w-[512px]",
  wide: "sm:w-[90dvw] md:w-[720px] lg:w-[960px]",
};

/** The parts of a dialog's cancel and close events its handlers read. */
interface DialogEvent {
  target: EventTarget | null;
  currentTarget: EventTarget | null;
  preventDefault: () => void;
}

/**
 * The <dialog>'s cancel (Escape) and close handlers. React dispatches these two events along the component tree, not the DOM, so a dialog opened inside another (a confirmation over a settings modal) delivers its own cancel and close to the outer one's handlers too; each handler acts only on events of its own element, or closing the confirmation would close the modal under it.
 *
 * Escape closes the dialog unless disableCloseOnOutsideClick guards it; allowEscapeClose lifts that guard for Escape alone, for a confirmation that a stray click must not dismiss but the keyboard's dismiss key should.
 */
export const dialogEventHandlers = ({
  open,
  onOpenChange,
  disableCloseOnOutsideClick,
  allowEscapeClose = false,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  disableCloseOnOutsideClick: boolean;
  allowEscapeClose?: boolean;
}) => ({
  onCancel: (e: DialogEvent) => {
    if (e.target !== e.currentTarget) return;
    e.preventDefault();
    if (!disableCloseOnOutsideClick || allowEscapeClose) onOpenChange(false);
  },
  onClose: (e: DialogEvent) => {
    if (e.target !== e.currentTarget) return;
    if (open) onOpenChange(false);
  },
});

interface DialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
  width?: keyof typeof WIDTHS;
  hideCloseButton?: boolean;
  /** Keeps a click on the backdrop and Escape from closing it, for dialogs that hold unsaved input. */
  disableCloseOnOutsideClick?: boolean;
  /** Lets Escape close the dialog even when disableCloseOnOutsideClick is set; the backdrop stays guarded. */
  allowEscapeClose?: boolean;
  /** alertdialog for a confirmation that interrupts the user, such as a delete. */
  role?: "dialog" | "alertdialog";
  className?: string;
}

export const Dialog = ({
  open,
  onOpenChange,
  children,
  width = "default",
  hideCloseButton = false,
  disableCloseOnOutsideClick = false,
  allowEscapeClose = false,
  role,
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
    if (open && !dialog.open) {
      dialog.showModal();
      // showModal() focuses the first focusable node, which is often a wrapper (cmdk's root) rather than the field the dialog is for; React's autoFocus ran before it and lost. A descendant marked data-autofocus takes the focus instead.
      dialog.querySelector<HTMLElement>("[data-autofocus]")?.focus();
    }
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
      role={role}
      aria-labelledby={titleId}
      aria-describedby={descriptionId}
      {...dialogEventHandlers({ open, onOpenChange, disableCloseOnOutsideClick, allowEscapeClose })}
      // The edge is a ring, not a border: the scroll container inherits the dialog's max-height, and a border would leave it 2px taller than the box that holds it.
      className={cn(
        "m-auto max-h-[90dvh] w-full max-w-[calc(100vw-2rem)] overflow-visible rounded-lg bg-white p-0 text-left text-slate-900 shadow-lg ring-1 ring-gray-200",
        WIDTHS[width],
        className
      )}>
      {open && (
        <DialogContext.Provider value={{ titleId, descriptionId }}>
          <PortalContainer.Provider value={element}>
            {/* The content follows the dialog's own max-height (a caller may lower it), so nothing spills past the panel. A DialogBody scrolls by itself between a fixed header and footer; content not wrapped in one scrolls here, with the header and footer held in place. The scroller takes the dialog's radius so its clip follows the rounded corners: the header's and footer's white backing reaches its edge and would otherwise show as square corners outside the ring. */}
            <div className="relative flex max-h-[inherit] flex-col gap-4 overflow-y-auto rounded-[inherit] p-4">
              {children}
            </div>
            {/* Outside the scroller, so it stays in the corner however far the content scrolls. */}
            {!hideCloseButton && (
              <button
                type="button"
                onClick={() => onOpenChange(false)}
                className="focus-visible:ring-ring absolute top-3 right-3 z-20 rounded-xs bg-transparent opacity-70 transition-opacity hover:opacity-100 focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-hidden">
                <X className="size-4 text-slate-500" />
                <span className="sr-only">{t("common.close")}</span>
              </button>
            )}
            <Toaster />
          </PortalContainer.Provider>
        </DialogContext.Provider>
      )}
    </dialog>
  );
};

// The header and footer stick to the scroller's padding edge, so they keep the panel's 1rem gutter, and a white backing reaching into that gutter hides the content scrolling past them. The backing is a pseudo-element, so the boxes keep their size and a caller's own widths and paddings lay out as before.
const STICKY_BACKING = "z-10 bg-white before:absolute before:-inset-x-4 before:-z-10 before:bg-white";

export const DialogHeader = ({ className, ...props }: HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      "sticky top-0 flex shrink-0 flex-col gap-y-1 pr-6 text-left before:-top-4 before:bottom-0",
      STICKY_BACKING,
      "[&>svg]:absolute [&>svg]:size-4 [&>svg]:text-ink [&>svg~*]:min-h-4 [&>svg~*]:pl-6",
      className
    )}
    {...props}
  />
);

export const DialogTitle = ({ className, children, ...props }: HTMLAttributes<HTMLHeadingElement>) => {
  const ctx = useContext(DialogContext);
  return (
    <h2 id={ctx?.titleId} className={cn("text-sm leading-none font-medium text-ink", className)} {...props}>
      {children}
    </h2>
  );
};

export const DialogDescription = ({ className, ...props }: HTMLAttributes<HTMLParagraphElement>) => {
  const ctx = useContext(DialogContext);
  return <p id={ctx?.descriptionId} className={cn("text-sm text-slate-500", className)} {...props} />;
};

/** The part of a dialog that scrolls when the content outgrows the panel. Its 4px inset leaves room for the focus rings of the fields at its edges, which the scroller would otherwise clip. */
export const DialogBody = ({ className, ...props }: HTMLAttributes<HTMLElement>) => (
  <section
    className={cn("-mx-1 -my-1 min-h-0 flex-1 overflow-y-auto px-1 py-1 text-sm", className)}
    {...props}
  />
);

export const DialogFooter = ({ className, ...props }: HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      "sticky bottom-0 flex shrink-0 flex-col-reverse gap-2 before:top-0 before:-bottom-4 sm:flex-row sm:justify-end",
      STICKY_BACKING,
      className
    )}
    {...props}
  />
);
