// A small notification store for the Toaster: forma2 used react-hot-toast, whose API (success, error, loading, and an id to replace a toast in place) this keeps.

export type ToastKind = "success" | "error" | "loading";

export interface Toast {
  id: string;
  kind: ToastKind;
  message: string;
}

interface ToastOptions {
  /** Replaces the toast with this id instead of adding another. */
  id?: string;
}

type Listener = () => void;

/** How long a toast stays up; a loading toast stays until it is replaced or dismissed. */
export const TOAST_DURATION_MS: Record<ToastKind, number | null> = {
  success: 4000,
  error: 6000,
  loading: null,
};

export const createToastStore = (schedule: (fn: () => void, ms: number) => unknown = setTimeout) => {
  let toasts: Toast[] = [];
  let nextId = 0;
  const listeners = new Set<Listener>();
  const emit = () => {
    for (const listener of listeners) listener();
  };

  const dismiss = (id: string) => {
    const remaining = toasts.filter((toast) => toast.id !== id);
    if (remaining.length === toasts.length) return;
    toasts = remaining;
    emit();
  };

  const show = (kind: ToastKind, message: string, options: ToastOptions = {}): string => {
    const id = options.id ?? `toast-${++nextId}`;
    const toast: Toast = { id, kind, message };
    const index = toasts.findIndex((existing) => existing.id === id);
    toasts =
      index === -1 ? [...toasts, toast] : toasts.map((existing, i) => (i === index ? toast : existing));
    emit();
    const duration = TOAST_DURATION_MS[kind];
    if (duration !== null) {
      schedule(() => {
        // Only the toast as shown here expires; a replacement under the same id has its own timer.
        if (toasts.find((existing) => existing.id === id) === toast) dismiss(id);
      }, duration);
    }
    return id;
  };

  return {
    success: (message: string, options?: ToastOptions) => show("success", message, options),
    error: (message: string, options?: ToastOptions) => show("error", message, options),
    loading: (message: string, options?: ToastOptions) => show("loading", message, options),
    dismiss,
    getSnapshot: (): Toast[] => toasts,
    subscribe: (listener: Listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
};

export const toast = createToastStore();
