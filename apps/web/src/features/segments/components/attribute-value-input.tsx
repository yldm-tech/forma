import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../../../lib/cn";
import { InputCombobox, TComboboxOption } from "../../../components/ui/input-combo-box";
import { attributeValuesQuery } from "../api";

/** How many values the server suggests at most (GET /contact-attribute-keys/:id/values); a full list may leave out the value searched for. */
const MAX_SUGGESTED_VALUES = 100;

interface AttributeValueInputProps {
  attributeKeyId: string;
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  className?: string;
  valueError?: string;
}

export const AttributeValueInput = ({
  attributeKeyId,
  value,
  onChange,
  disabled,
  className,
  valueError,
}: AttributeValueInputProps) => {
  const { t } = useTranslation();
  // The values contacts already have for the key, offered as suggestions; a failed load leaves only free text.
  const { data: values, isFetching: loading } = useQuery({
    ...attributeValuesQuery(attributeKeyId),
    enabled: attributeKeyId !== "",
  });
  const options = useMemo<TComboboxOption[]>(
    () => (values ?? []).map((val) => ({ label: val, value: val })),
    [values]
  );

  const emptyDropdownText = useMemo(() => {
    if (loading) return t("workspace.segments.loading_values");
    if (options.length === MAX_SUGGESTED_VALUES)
      return t("workspace.segments.showing_first_values", { count: MAX_SUGGESTED_VALUES });
    return t("workspace.segments.no_values_found");
  }, [loading, options.length, t]);

  return (
    <div className="relative">
      <InputCombobox
        id={`attribute-value-${attributeKeyId}`}
        options={options}
        value={value}
        onChangeValue={(newValue) => onChange(newValue as string)}
        aria-label={t("workspace.segments.filter_value")}
        withInput={true}
        showSearch={true}
        clearable={true}
        inputProps={{
          "aria-label": t("workspace.segments.filter_value"),
          className: cn(
            "h-9 w-auto bg-white",
            valueError && "border border-red-500 focus:border-red-500",
            className
          ),
          disabled,
        }}
        comboboxClasses="h-9"
        emptyDropdownText={emptyDropdownText}
      />

      {valueError && (
        <p className="absolute right-2 -mt-1 rounded-md bg-white px-2 text-xs text-red-500">{valueError}</p>
      )}
    </div>
  );
};
