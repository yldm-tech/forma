import { type ShouldBlockFn, useBlocker } from "@tanstack/react-router";
import { type ReactNode, createContext, useCallback, useContext, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { ConfirmationModal } from "../ui/confirmation-modal";

/** Registers one form with unsaved edits and returns the function that withdraws it. */
type RegisterDirtyForm = () => () => void;

const UnsavedChangesContext = createContext<RegisterDirtyForm | null>(null);

/**
 * Marks the calling form as holding edits that are not saved while `isDirty` is true, so that leaving the page asks first (see UnsavedChangesGuard). Outside a guard (a form rendered on its own, as in a test) it does nothing.
 */
export const useUnsavedChanges = (isDirty: boolean) => {
  const register = useContext(UnsavedChangesContext);
  useEffect(() => {
    if (!register || !isDirty) return;
    return register();
  }, [register, isDirty]);
};

/**
 * Asks "Discard changes?" before the user leaves a page while any form under it reports unsaved edits through useUnsavedChanges: an in-app navigation to another page waits for the answer, and a reload, a closed tab or a full navigation gets the browser's own leave-page prompt. One guard serves the whole page, so a page with several edited forms asks once. A navigation that only changes the search or the hash stays on the page and is not asked about.
 */
export const UnsavedChangesGuard = ({ children }: { children: ReactNode }) => {
  const { t } = useTranslation();
  // A set of tokens rather than a count: a form that withdraws twice (an effect cleanup after an unmount) must not withdraw another form's edits.
  const dirtyForms = useRef(new Set<symbol>());
  const register = useCallback<RegisterDirtyForm>(() => {
    const token = Symbol("dirty form");
    dirtyForms.current.add(token);
    return () => {
      dirtyForms.current.delete(token);
    };
  }, []);
  const hasUnsavedChanges = useCallback(() => dirtyForms.current.size > 0, []);
  const shouldBlockFn = useCallback<ShouldBlockFn>(
    ({ current, next }) => hasUnsavedChanges() && current.pathname !== next.pathname,
    [hasUnsavedChanges]
  );
  const blocker = useBlocker({
    shouldBlockFn,
    enableBeforeUnload: hasUnsavedChanges,
    withResolver: true,
  });

  return (
    <UnsavedChangesContext.Provider value={register}>
      {children}
      <ConfirmationModal
        open={blocker.status === "blocked"}
        setOpen={(open) => {
          if (!open) blocker.reset?.();
        }}
        title={t("common.discard_changes_title")}
        description={t("common.discard_changes_description")}
        body={null}
        buttonText={t("common.discard_changes_confirm")}
        cancelButtonText={t("common.discard_changes_keep_editing")}
        onConfirm={() => blocker.proceed?.()}
      />
    </UnsavedChangesContext.Provider>
  );
};
