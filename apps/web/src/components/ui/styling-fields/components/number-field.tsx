import type { FieldPath, FieldValues, UseFormReturn } from "react-hook-form";
import { FormControl, FormDescription, FormField, FormItem, FormLabel } from "../../form";
import { Input } from "../../input";

interface NumberFieldProps<T extends FieldValues> {
  form: UseFormReturn<T>;
  name: FieldPath<T>;
  label: string;
  description?: string;
  step?: number;
  min?: number;
  max?: number;
  placeholder?: string;
}

export const NumberField = <T extends FieldValues>({
  form,
  name,
  label,
  description,
  placeholder,
  step = 1,
  min,
  max,
}: NumberFieldProps<T>) => (
  <FormField
    control={form.control}
    name={name}
    render={({ field }) => (
      <FormItem className="space-y-1">
        <FormLabel>{label}</FormLabel>
        {description && <FormDescription>{description}</FormDescription>}
        <FormControl>
          <Input
            type="number"
            {...field}
            onChange={(e) => {
              const val = e.target.valueAsNumber;
              field.onChange(Number.isNaN(val) ? null : val);
            }}
            step={step}
            min={min}
            max={max}
            placeholder={placeholder}
          />
        </FormControl>
      </FormItem>
    )}
  />
);
