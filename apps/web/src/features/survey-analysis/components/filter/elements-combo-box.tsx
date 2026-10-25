import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import type { TFunction } from "i18next";
import {
  AirplayIcon,
  ArrowUpFromDotIcon,
  CheckIcon,
  ChevronDown,
  ChevronUp,
  ContactIcon,
  EyeOff,
  FlagIcon,
  GaugeIcon,
  GlobeIcon,
  GridIcon,
  HashIcon,
  HomeIcon,
  ImageIcon,
  LanguagesIcon,
  LinkIcon,
  ListIcon,
  ListOrderedIcon,
  type LucideIcon,
  MessageSquareTextIcon,
  MousePointerClickIcon,
  NetworkIcon,
  PieChartIcon,
  PresentationIcon,
  Rows3Icon,
  SmartphoneIcon,
  SmilePlusIcon,
  SquareFunctionIcon,
  StarIcon,
  User,
} from "lucide-react";
import { Fragment, useRef, useState } from "react";
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
import { cn } from "../../../../lib/cn";
import { useClickOutside } from "../../../../lib/use-click-outside";
import { type ElementOption, type ElementOptions, OptionsType } from "../../lib/filter-types";
import { getFilterTargetLabel } from "../../lib/filters";
import { RESERVED_FIELD_ICONS } from "../../lib/reserved-fields";

const getOptionsTypeLabel = (type: OptionsType, t: TFunction): string => {
  switch (type) {
    case OptionsType.ELEMENTS:
      return t("common.elements");
    case OptionsType.TAGS:
      return t("common.tags");
    case OptionsType.ATTRIBUTES:
      return t("common.attributes");
    case OptionsType.OTHERS:
      return t("common.other_filters");
    case OptionsType.META:
      return t("common.meta");
    case OptionsType.HIDDEN_FIELDS:
      return t("common.hidden_fields");
    case OptionsType.VARIABLES:
      return t("common.variables");
    case OptionsType.QUOTAS:
      return t("common.quotas");
  }
};

const ELEMENT_ICONS: Record<string, LucideIcon> = {
  [TSurveyElementTypeEnum.OpenText]: MessageSquareTextIcon,
  [TSurveyElementTypeEnum.Rating]: StarIcon,
  [TSurveyElementTypeEnum.CTA]: MousePointerClickIcon,
  [TSurveyElementTypeEnum.MultipleChoiceMulti]: ListIcon,
  [TSurveyElementTypeEnum.MultipleChoiceSingle]: Rows3Icon,
  [TSurveyElementTypeEnum.NPS]: PresentationIcon,
  [TSurveyElementTypeEnum.Consent]: CheckIcon,
  [TSurveyElementTypeEnum.PictureSelection]: ImageIcon,
  [TSurveyElementTypeEnum.Matrix]: GridIcon,
  [TSurveyElementTypeEnum.Ranking]: ListOrderedIcon,
  [TSurveyElementTypeEnum.CSAT]: SmilePlusIcon,
  [TSurveyElementTypeEnum.CES]: GaugeIcon,
  [TSurveyElementTypeEnum.Address]: HomeIcon,
  [TSurveyElementTypeEnum.ContactInfo]: ContactIcon,
  [OptionsType.ATTRIBUTES]: User,
  [OptionsType.HIDDEN_FIELDS]: EyeOff,
  [OptionsType.VARIABLES]: SquareFunctionIcon,
  // Reserved fields resolve through RESERVED_FIELD_ICONS by catalog name; these are fallbacks for ids the shared map has no icon for.
  device: SmartphoneIcon,
  os: AirplayIcon,
  browser: GlobeIcon,
  source: ArrowUpFromDotIcon,
  action: MousePointerClickIcon,
  country: FlagIcon,
  url: LinkIcon,
  ipAddress: NetworkIcon,
  Language: LanguagesIcon,
  [OptionsType.TAGS]: HashIcon,
  [OptionsType.QUOTAS]: PieChartIcon,
};

const renderIcon = (key: string) => {
  const Icon = Object.hasOwn(ELEMENT_ICONS, key) ? ELEMENT_ICONS[key] : undefined;
  return Icon ? <Icon className="size-5" strokeWidth={1.5} /> : null;
};

const ICON_BACKGROUNDS: Record<string, string> = {
  [OptionsType.ATTRIBUTES]: "bg-indigo-500",
  [OptionsType.ELEMENTS]: "bg-slate-700",
  [OptionsType.TAGS]: "bg-indigo-500",
  [OptionsType.QUOTAS]: "bg-slate-500",
};

const getLabelClassName = (type: string, label?: string): string => {
  if (type !== OptionsType.META) return "";
  return label === "os" || label === "url" ? "uppercase" : "capitalize";
};

/** One option of the filter picker: its group's icon and its label. */
export const SelectedCommandItem = ({ label, elementType, type, id }: Partial<ElementOption>) => {
  const { t } = useTranslation();
  const getDisplayIcon = () => {
    if (!type) return null;
    if (type === OptionsType.ELEMENTS && elementType) return renderIcon(elementType);
    if (type === OptionsType.META && id) {
      const ReservedIcon = RESERVED_FIELD_ICONS[id];
      if (ReservedIcon) return <ReservedIcon className="size-5" strokeWidth={1.5} />;
      return renderIcon(id);
    }
    if (type === OptionsType.OTHERS && label) return renderIcon(label);
    if (
      type === OptionsType.ATTRIBUTES ||
      type === OptionsType.HIDDEN_FIELDS ||
      type === OptionsType.VARIABLES ||
      type === OptionsType.TAGS ||
      type === OptionsType.QUOTAS
    ) {
      return renderIcon(type);
    }
    return null;
  };

  return (
    <div className="flex h-full min-w-0 items-center gap-2">
      <span
        className={cn(
          "flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-white",
          ICON_BACKGROUNDS[type ?? ""] ?? "bg-amber-500"
        )}>
        {getDisplayIcon()}
      </span>
      <p className={cn("truncate text-sm text-slate-600", getLabelClassName(type ?? "", label))}>
        {getFilterTargetLabel({ type, id, label }, t)}
      </p>
    </div>
  );
};

/** forma2's ElementsComboBox: the searchable picker of what a filter row filters on, grouped by kind. */
export const ElementsComboBox = ({
  options,
  selected,
  onChangeValue,
}: {
  options: ElementOptions[];
  selected: Partial<ElementOption>;
  onChangeValue: (option: ElementOption) => void;
}) => {
  const [open, setOpen] = useState(false);
  const { t } = useTranslation();
  const commandRef = useRef<HTMLDivElement>(null);
  const [inputValue, setInputValue] = useState("");
  useClickOutside(commandRef, () => setOpen(false));

  const hasSelection = Object.hasOwn(selected, "label");
  const ChevronIcon = open ? ChevronUp : ChevronDown;

  return (
    <Command
      ref={commandRef}
      className="relative h-fit w-full overflow-visible rounded-md border border-slate-300 bg-white hover:border-slate-400">
      <div
        role="button"
        tabIndex={0}
        className="flex cursor-pointer items-center justify-between"
        onClick={() => !open && setOpen(true)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            if (!open) setOpen(true);
          }
        }}>
        {!open && hasSelection && <SelectedCommandItem {...selected} />}
        {(open || !hasSelection) && (
          <CommandInput
            value={inputValue}
            onValueChange={setInputValue}
            placeholder={open ? `${t("common.search")}...` : t("common.select_filter")}
            className="max-w-full grow border-none p-0 pl-2 text-sm shadow-none ring-offset-transparent outline-hidden focus:border-none focus:shadow-none focus:ring-offset-0 focus:outline-hidden"
          />
        )}
        <Button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            setOpen(!open);
          }}
          variant="secondary"
          size="icon"
          className="shrink-0"
          aria-expanded={open}
          aria-label={t("common.select")}>
          <ChevronIcon className="size-4 opacity-50" />
        </Button>
      </div>

      {open && (
        <div className="absolute top-full z-10 mt-1 w-full overflow-auto rounded-md bg-white shadow-md outline-hidden">
          <CommandList className="max-h-[600px]">
            <CommandEmpty>{t("common.no_result_found")}</CommandEmpty>
            {options.map((group) => (
              <Fragment key={group.header}>
                {group.option.length > 0 && (
                  <CommandGroup
                    heading={
                      <p className="text-sm font-medium text-slate-600">
                        {getOptionsTypeLabel(group.header, t)}
                      </p>
                    }>
                    {group.option.map((option) => (
                      <CommandItem
                        key={option.id}
                        onSelect={() => {
                          setInputValue("");
                          onChangeValue(option);
                          setOpen(false);
                        }}>
                        <SelectedCommandItem {...option} />
                      </CommandItem>
                    ))}
                  </CommandGroup>
                )}
              </Fragment>
            ))}
          </CommandList>
        </div>
      )}
    </Command>
  );
};
