import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { cn } from "../../../lib/cn";
import { InputCombobox, TComboboxOption } from "../../../components/ui/input-combo-box";
import { attributeValuesQuery } from "../api";

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
    if (loading) return "Loading values...";
    if (options.length === 50) return "Showing first 50 values";
    return "No values found";
  }, [loading, options.length]);

  return (
    <div className="relative">
      <InputCombobox
        id={`attribute-value-${attributeKeyId}`}
        options={options}
        value={value}
        onChangeValue={(newValue) => onChange(newValue as string)}
        withInput={true}
        showSearch={true}
        clearable={true}
        inputProps={{
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
