import type { TTag } from "@forma/types/tags";
import type { ReactNode } from "react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "../../../../components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "../../../../components/ui/popover";

/** A toolbar button that opens a searchable list of tags and hands the chosen one back; with onCreate, a search that names no tag offers to create it. */
export const BulkTagPicker = ({
  label,
  icon,
  tags,
  onPick,
  onCreate,
  emptyText,
  disabled,
  className,
}: {
  label: string;
  icon: ReactNode;
  tags: TTag[];
  onPick: (tag: TTag) => void;
  onCreate?: (name: string) => void;
  /** What the list says when there is no tag to offer. */
  emptyText: string;
  disabled: boolean;
  className: string;
}) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const name = search.trim();
  const canCreate =
    onCreate !== undefined &&
    name !== "" &&
    !tags.some((tag) => tag.name.toLowerCase() === name.toLowerCase());

  const close = () => {
    setOpen(false);
    setSearch("");
  };

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (next) setOpen(true);
        else close();
      }}>
      <PopoverTrigger asChild disabled={disabled}>
        <Button variant="ghost" size="sm" className={`${className} gap-1`} aria-expanded={open}>
          {icon}
          {label}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[220px] p-0" align="start">
        {/* cmdk names its input after the command's label, which it renders hidden. */}
        <Command label={t("workspace.tags.search_tags")}>
          <div className="px-1 pt-1">
            <CommandInput
              placeholder={t("workspace.tags.search_tags")}
              className="h-8 border-none py-1 shadow-none outline-0"
              value={search}
              onValueChange={setSearch}
            />
          </div>
          <CommandList className="max-h-52 border-0">
            {!canCreate && (
              <CommandEmpty className="px-2 py-4 text-center text-sm text-slate-500">
                {tags.length === 0 ? emptyText : t("workspace.tags.no_tag_found")}
              </CommandEmpty>
            )}
            <CommandGroup className="p-1">
              {tags.map((tag) => (
                <CommandItem
                  key={tag.id}
                  value={tag.id}
                  keywords={[tag.name]}
                  onSelect={() => {
                    close();
                    onPick(tag);
                  }}>
                  <span className="truncate">{tag.name}</span>
                </CommandItem>
              ))}
              {canCreate && (
                <CommandItem
                  forceMount
                  value="_create"
                  onSelect={() => {
                    close();
                    onCreate(name);
                  }}>
                  <span className="truncate">
                    + {t("workspace.surveys.responses.bulk.create_tag", { name })}
                  </span>
                </CommandItem>
              )}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
};
