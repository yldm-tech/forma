import type { FieldPath, FieldValues, UseFormReturn } from "react-hook-form";
import { FormControl, FormDescription, FormField, FormItem, FormLabel } from "../../form";
import { Input } from "../../input";

interface TextFieldProps<T extends FieldValues> {
  form: UseFormReturn<T>;
  name: FieldPath<T>;
  label: string;
  description?: string;
}

export const TextField = <T extends FieldValues>({ form, name, label, description }: TextFieldProps<T>) => (
  <FormField
    control={form.control}
    name={name}
    render={({ field }) => (
      <FormItem className="space-y-1">
        <FormLabel>{label}</FormLabel>
        {description && <FormDescription>{description}</FormDescription>}
        <FormControl>
          <Input type="text" {...field} />
        </FormControl>
      </FormItem>
    )}
  />
);
