// A small notification store for the Toaster: forma2 used react-hot-toast, whose API (success, error, loading, and an id to replace a toast in place) this keeps.

export type ToastKind = "success" | "error" | "loading";

/** A button on the toast that follows up on what it reports (Undo, Open); pressing it also dismisses the toast. */
export interface ToastAction {
  label: string;
  onClick: () => void;
}

export interface Toast {
  id: string;
  kind: ToastKind;
  message: string;
  action?: ToastAction;
}

interface ToastOptions {
  /** Replaces the toast with this id instead of adding another. */
  id?: string;
  action?: ToastAction;
}

type Listener = () => void;

/** How long a toast stays up; a loading toast stays until it is replaced or dismissed. */
export const TOAST_DURATION_MS: Record<ToastKind, number | null> = {
  success: 4000,
  error: 6000,
  loading: null,
};

/** A toast with an action stays longer than its kind's duration, so there is time to reach its button. Hovering or focusing a toast also stops its clock (see pause), and what an action does stays reachable elsewhere: an archived survey is restored from the list's Archived filter, a copy opened from the list. */
export const ACTION_TOAST_DURATION_MS = 10000;

export const createToastStore = (schedule: (fn: () => void, ms: number) => unknown = setTimeout) => {
  let toasts: Toast[] = [];
  let nextId = 0;
  const listeners = new Set<Listener>();
  const emit = () => {
    for (const listener of listeners) listener();
  };
  // Each shown toast's duration, and the timer currently counting it down (a pause or a replacement starts a new one, and an older timer that fires finds it is no longer current).
  const durations = new WeakMap<Toast, number>();
  const currentTimer = new WeakMap<Toast, number>();
  const paused = new Set<string>();
  let nextTimer = 0;

  const dismiss = (id: string) => {
    const remaining = toasts.filter((toast) => toast.id !== id);
    if (remaining.length === toasts.length) return;
    toasts = remaining;
    paused.delete(id);
    emit();
  };

  /** Starts the toast's full duration again, unless it has none or is paused. */
  const startTimer = (toast: Toast) => {
    const duration = durations.get(toast);
    if (duration === undefined || paused.has(toast.id)) return;
    const timer = ++nextTimer;
    currentTimer.set(toast, timer);
    schedule(() => {
      // Only the toast as shown here expires, and only by its latest timer; a replacement under the same id has its own.
      if (currentTimer.get(toast) !== timer || paused.has(toast.id)) return;
      currentTimer.delete(toast);
      durations.delete(toast);
      if (toasts.find((existing) => existing.id === toast.id) === toast) dismiss(toast.id);
    }, duration);
  };

  const show = (kind: ToastKind, message: string, options: ToastOptions = {}): string => {
    const id = options.id ?? `toast-${++nextId}`;
    const toast: Toast = options.action
      ? { id, kind, message, action: options.action }
      : { id, kind, message };
    const index = toasts.findIndex((existing) => existing.id === id);
    toasts =
      index === -1 ? [...toasts, toast] : toasts.map((existing, i) => (i === index ? toast : existing));
    emit();
    const kindDuration = TOAST_DURATION_MS[kind];
    const duration = options.action && kindDuration !== null ? ACTION_TOAST_DURATION_MS : kindDuration;
    if (duration !== null) {
      durations.set(toast, duration);
      startTimer(toast);
    }
    return id;
  };

  /** Stops the toast's clock while the pointer is over it or the focus is in it, so it does not vanish under the user. */
  const pause = (id: string) => {
    paused.add(id);
  };

  /** Lets the toast's clock run again, from its full duration. */
  const resume = (id: string) => {
    if (!paused.delete(id)) return;
    const toast = toasts.find((existing) => existing.id === id);
    if (toast) startTimer(toast);
  };

  return {
    success: (message: string, options?: ToastOptions) => show("success", message, options),
    error: (message: string, options?: ToastOptions) => show("error", message, options),
    loading: (message: string, options?: ToastOptions) => show("loading", message, options),
    dismiss,
    pause,
    resume,
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

/**
 * The places a Toaster is mounted, newest last: the page's own, and one inside each open modal dialog. Only the newest shows the toasts. A modal <dialog> puts itself in the top layer and makes the rest of the page inert, so a toast drawn outside it would sit under the backdrop, could not be dismissed and would not be announced; drawn inside the topmost dialog it is above the backdrop and live.
 */
export const createToasterHosts = () => {
  let hosts: string[] = [];
  const listeners = new Set<Listener>();
  const emit = () => {
    for (const listener of listeners) listener();
  };
  return {
    /** Adds a host on top and returns the function that removes it. */
    register: (id: string) => {
      hosts = [...hosts.filter((host) => host !== id), id];
      emit();
      return () => {
        hosts = hosts.filter((host) => host !== id);
        emit();
      };
    },
    /** The host that shows the toasts, or null when none is mounted. */
    getSnapshot: (): string | null => hosts.at(-1) ?? null,
    subscribe: (listener: Listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
};

export const toasterHosts = createToasterHosts();
