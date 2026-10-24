import { getLanguageLabel } from "@forma/i18n-utils/utils";
import { RESERVED_FIELD_CATALOG, listDisplayableReservedFields } from "@forma/types/embedded-data-resolver";
import type { TResponse } from "@forma/types/responses";
import { LanguagesIcon, type LucideIcon, MonitorIcon, SmartphoneIcon, Tag } from "lucide-react";
import { Fragment, type ReactNode, useId } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "../../../../components/ui/popover";
import { getReservedFieldLabel } from "../../lib/reserved-fields";

/** An icon button that opens a popover rather than a tooltip, so its rows can be reached from a keyboard or a touch screen and selected. */
const InfoPopoverButton = ({
  icon: Icon,
  title,
  children,
}: {
  icon: LucideIcon;
  title: string;
  children: ReactNode;
}) => {
  const headingId = useId();
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button type="button" variant="outline" size="icon" aria-label={title}>
          <Icon className="size-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        side="bottom"
        aria-labelledby={headingId}
        className="max-h-[70vh] w-auto max-w-[min(28rem,90vw)] overflow-y-auto">
        <h4 id={headingId} className="mb-3 text-sm font-semibold text-slate-700">
          {title}
        </h4>
        {children}
      </PopoverContent>
    </Popover>
  );
};

/** Key and value pairs as a definition list, values wrapping so a long URL reads in full. */
const KeyValueList = ({ items }: { items: { key: string; label: string; value: string }[] }) => (
  <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-sm">
    {items.map(({ key, label, value }) => (
      <Fragment key={key}>
        <dt className="text-slate-500">{label}</dt>
        <dd className="ph-no-capture min-w-0 font-medium wrap-anywhere text-slate-700">{value}</dd>
      </Fragment>
    ))}
  </dl>
);

/**
 * forma2's SingleResponseCardMetadata: the contact's attributes, the auto-captured fields and the survey language, each behind an icon. The fields come from the catalog, primary ones first; an absent field is left out, so a response captured anonymized shows only what it holds.
 */
export const ResponseCardMetadata = ({ response, locale }: { response: TResponse; locale: string }) => {
  const { t } = useTranslation();
  const hasContactAttributes = Boolean(
    response.contactAttributes && Object.keys(response.contactAttributes).length > 0
  );
  const primaryFields = listDisplayableReservedFields(RESERVED_FIELD_CATALOG, response, "primary");
  const secondaryFields = listDisplayableReservedFields(RESERVED_FIELD_CATALOG, response, "secondary");
  const hasMetadata = primaryFields.length > 0 || secondaryFields.length > 0;
  const language = response.language && response.language !== "default" ? response.language : null;

  if (!hasContactAttributes && !hasMetadata && !language) return null;

  const device = response.meta.userAgent?.device?.toLowerCase();
  const deviceIcon =
    device && (device.includes("mobile") || device.includes("phone")) ? SmartphoneIcon : MonitorIcon;

  const toItems = (fields: typeof primaryFields) =>
    fields.map(({ entry, value }) => ({
      key: entry.name,
      label: getReservedFieldLabel(entry.name, t),
      value,
    }));

  return (
    <div className="flex items-center gap-x-2">
      {hasContactAttributes && (
        <InfoPopoverButton icon={Tag} title={t("workspace.surveys.responses.person_attributes")}>
          {response.singleUseId && (
            <>
              <KeyValueList
                items={[
                  {
                    key: "singleUseId",
                    label: t("workspace.surveys.responses.single_use_id"),
                    value: response.singleUseId,
                  },
                ]}
              />
              <hr className="my-3 border-slate-200" />
            </>
          )}
          <KeyValueList
            items={Object.entries(response.contactAttributes ?? {}).map(([key, value]) => ({
              key,
              label: key,
              value: String(value),
            }))}
          />
        </InfoPopoverButton>
      )}
      {hasMetadata && (
        <InfoPopoverButton icon={deviceIcon} title={t("common.metadata")}>
          {primaryFields.length > 0 && <KeyValueList items={toItems(primaryFields)} />}
          {primaryFields.length > 0 && secondaryFields.length > 0 && <hr className="my-3 border-slate-200" />}
          {secondaryFields.length > 0 && <KeyValueList items={toItems(secondaryFields)} />}
        </InfoPopoverButton>
      )}
      {language && (
        <InfoPopoverButton icon={LanguagesIcon} title={t("workspace.surveys.responses.survey_language")}>
          <p className="text-sm font-medium text-slate-700">{getLanguageLabel(language, locale)}</p>
        </InfoPopoverButton>
      )}
    </div>
  );
};
