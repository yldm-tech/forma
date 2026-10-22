import { useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import {
  Command,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "../../../../components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "../../../../components/ui/popover";

interface TagOption {
  label: string;
  value: string;
}

/** forma2's TagsCombobox: pick one of the workspace's tags for a response, or create one from the search text. */
export const TagsCombobox = ({
  tags,
  currentTags,
  addTag,
  createTag,
  searchValue,
  setSearchValue,
  open,
  setOpen,
}: {
  tags: TagOption[];
  currentTags: TagOption[];
  addTag: (tagId: string) => void;
  createTag: (tagName: string) => void;
  searchValue: string;
  setSearchValue: (value: string) => void;
  open: boolean;
  setOpen: (open: boolean) => void;
}) => {
  const { t } = useTranslation();
  const tagsToSearch = useMemo(
    () =>
      tags.filter(
        (tag) => !currentTags.some((current) => current.value.toLowerCase() === tag.value.toLowerCase())
      ),
    [currentTags, tags]
  );

  useEffect(() => {
    if (!open) setSearchValue("");
  }, [open, setSearchValue]);

  const trimmedSearchValue = searchValue.trim();
  const nameIsTaken =
    currentTags.some((tag) => tag.label === trimmedSearchValue) ||
    tagsToSearch.some((tag) => tag.label === trimmedSearchValue);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button aria-expanded={open}>{t("workspace.tags.add_tag")}</Button>
      </PopoverTrigger>
      <PopoverContent className="max-h-60 w-[200px] overflow-y-auto p-0">
        <Command
          filter={(value, search) => {
            if (value === "_create") return 1;
            const label = tagsToSearch.find((tag) => tag.value.toLowerCase() === value)?.label ?? "";
            return label.toLowerCase().includes(search.toLowerCase()) ? 1 : 0;
          }}>
          <div className="px-1 pt-1">
            <CommandInput
              placeholder={
                tagsToSearch.length === 0 ? t("workspace.tags.add_tag") : t("workspace.tags.search_tags")
              }
              className="h-8 border-none py-1 shadow-none outline-0"
              value={searchValue}
              onValueChange={setSearchValue}
              onKeyDown={(e) => {
                if (e.key === "Enter" && trimmedSearchValue !== "" && !nameIsTaken)
                  createTag(trimmedSearchValue);
              }}
            />
          </div>
          <CommandList className="border-0">
            <CommandGroup className="p-0">
              {tagsToSearch.map((tag) => (
                <CommandItem
                  key={tag.value}
                  value={tag.value}
                  onSelect={(currentValue) => {
                    setOpen(false);
                    addTag(currentValue);
                  }}
                  className="hover:cursor-pointer hover:bg-slate-50">
                  {tag.label}
                </CommandItem>
              ))}
              {trimmedSearchValue !== "" && !nameIsTaken && (
                <CommandItem value="_create" onSelect={() => createTag(trimmedSearchValue)}>
                  <span className="h-8 w-full text-left leading-8 hover:cursor-pointer">
                    + {t("workspace.tags.add")} {trimmedSearchValue}
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
