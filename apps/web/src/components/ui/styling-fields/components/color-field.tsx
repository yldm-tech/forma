import type { FieldPath, FieldValues, UseFormReturn } from "react-hook-form";
import { ColorPicker } from "../../color-picker/index";
import { FormControl, FormDescription, FormField, FormItem, FormLabel } from "../../form";

interface ColorFieldProps<T extends FieldValues> {
  form: UseFormReturn<T>;
  name: FieldPath<T>;
  label: string;
  description?: string;
  containerClass?: string;
}

export const ColorField = <T extends FieldValues>({
  form,
  name,
  label,
  description,
  containerClass,
}: ColorFieldProps<T>) => (
  <FormField
    control={form.control}
    name={name}
    render={({ field }) => (
      <FormItem className="space-y-1">
        <FormLabel>{label}</FormLabel>
        {description && <FormDescription>{description}</FormDescription>}
        <FormControl>
          <ColorPicker
            color={field.value ?? ""}
            onChange={(color) => field.onChange(color)}
            containerClass={containerClass || "w-full"}
          />
        </FormControl>
      </FormItem>
    )}
  />
);
