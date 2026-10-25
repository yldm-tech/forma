import { type ReactNode, useId, useState } from "react";
import { cn } from "../../lib/cn";
import { Dialog, DialogBody, DialogDescription, DialogHeader, DialogTitle } from "./dialog";

export interface ModalTab {
  title: string;
  children: ReactNode;
}

/** forma2's ModalWithTabs: a dialog whose body switches between tabs, opening on the first. The caller remounts it (or closes it) to start over on the first tab. */
export const ModalWithTabs = ({
  open,
  setOpen,
  tabs,
  icon,
  label,
  description,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  tabs: ModalTab[];
  icon?: ReactNode;
  label: string;
  description?: string;
}) => {
  const id = useId();
  const [activeTab, setActiveTab] = useState(0);
  const close = (next: boolean) => {
    if (!next) setActiveTab(0);
    setOpen(next);
  };
  const current = tabs[Math.min(activeTab, tabs.length - 1)];
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogHeader>
        {icon}
        <DialogTitle>{label}</DialogTitle>
        {description && <DialogDescription>{description}</DialogDescription>}
      </DialogHeader>
      <DialogBody>
        {tabs.length > 1 && (
          <div
            role="tablist"
            className="flex w-full items-center justify-center gap-x-2 border-b border-slate-200 px-6">
            {tabs.map((tab, index) => (
              <button
                type="button"
                role="tab"
                id={`${id}-tab-${index}`}
                aria-selected={activeTab === index}
                aria-controls={`${id}-panel`}
                key={tab.title}
                className={cn(
                  "mr-4 px-1 pb-3 focus:outline-hidden",
                  activeTab === index
                    ? "border-brand border-b-2 font-semibold text-slate-900"
                    : "text-slate-500 hover:text-slate-700"
                )}
                onClick={() => setActiveTab(index)}>
                {tab.title}
              </button>
            ))}
          </div>
        )}
        <div
          id={`${id}-panel`}
          role={tabs.length > 1 ? "tabpanel" : undefined}
          aria-labelledby={tabs.length > 1 ? `${id}-tab-${activeTab}` : undefined}
          className={tabs.length > 1 ? "flex-1 pt-4" : "flex-1"}>
          {current?.children}
        </div>
      </DialogBody>
    </Dialog>
  );
};
