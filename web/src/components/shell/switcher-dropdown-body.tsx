import { Building2Icon, FoldersIcon, Loader2, SettingsIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { DropdownMenuCheckboxItem, DropdownMenuGroup, DropdownMenuSeparator } from "../ui/dropdown-menu";

interface SwitcherDropdownBodyProps {
  type: "organization" | "workspace";
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
  items: { id: string; name: string }[];
  selectedId: string;
  onSelect: (id: string) => void;
  /** Rendered after the list once it has loaded, such as the add-workspace action. */
  children?: ReactNode;
  /** A Settings entry for the current item; omitted where the caller renders its own. */
  onSettings?: () => void;
}

/** forma2's SwitcherDropdownBody: header, loading state, load error with retry, the list, and an optional Settings footer. */
export const SwitcherDropdownBody = ({
  type,
  isLoading,
  isError,
  onRetry,
  items,
  selectedId,
  onSelect,
  children,
  onSettings,
}: SwitcherDropdownBodyProps) => {
  const { t } = useTranslation();
  const Icon = type === "organization" ? Building2Icon : FoldersIcon;
  return (
    <>
      <div className="px-2 py-1.5 text-sm font-medium text-slate-500">
        <Icon className="mr-2 inline size-4" strokeWidth={1.5} />
        {type === "organization" ? t("common.choose_organization") : t("common.choose_workspace")}
      </div>
      {isLoading && (
        <div className="flex items-center justify-center py-2">
          <Loader2 className="size-4 animate-spin" />
        </div>
      )}
      {!isLoading && isError && (
        <div className="px-2 py-4">
          <p className="mb-2 text-sm text-red-600">
            {type === "organization" ? t("common.failed_to_load_organizations") : t("common.failed_to_load_workspaces")}
          </p>
          <button type="button" onClick={onRetry} className="text-xs text-slate-600 underline hover:text-slate-800">
            {t("common.try_again")}
          </button>
        </div>
      )}
      {!isLoading && !isError && (
        <>
          <DropdownMenuGroup className="max-h-[300px] overflow-y-auto">
            {items.map((item) => (
              <DropdownMenuCheckboxItem key={item.id} checked={item.id === selectedId} onSelect={() => onSelect(item.id)}>
                {item.name}
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuGroup>
          {children}
        </>
      )}
      {onSettings && (
        <>
          <DropdownMenuSeparator />
          <DropdownMenuCheckboxItem onSelect={onSettings}>
            <SettingsIcon className="mr-2 size-4" strokeWidth={1.5} />
            {t("common.settings")}
          </DropdownMenuCheckboxItem>
        </>
      )}
    </>
  );
};
