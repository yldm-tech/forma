import * as React from "react";
import { getFormFieldAutocomplete } from "@/components/elements/autocomplete";
import { ElementError, getElementErrorAria } from "@/components/general/element-error";
import { ElementHeader } from "@/components/general/element-header";
import { Input } from "@/components/general/input";
import { Label } from "@/components/general/label";

/**
 * Form field configuration
 */
export interface FormFieldConfig {
  /** Unique identifier for the field */
  id: string;
  /** Label text for the field */
  label: string;
  /** Placeholder text for the input */
  placeholder?: string;
  /** Input type (text, email, tel, number, url, etc.) */
  type?: "text" | "email" | "tel" | "number" | "url";
  /** Whether this field is required */
  required?: boolean;
  /** Whether this field should be shown */
  show?: boolean;
  /**
   * HTML autofill token (WCAG 2.1 SC 1.3.5). Left unset, it is derived from `id` for the built-in
   * contact-info and address fields; a plain string so an author-facing control can set it later.
   */
  autocomplete?: string;
}

interface FormFieldProps {
  /** Unique identifier for the element container */
  elementId: string;
  /** The main element or prompt text displayed as the headline */
  headline: string;
  /** Optional descriptive text displayed below the headline */
  description?: string;
  /** Array of form field configurations */
  fields: FormFieldConfig[];
  /** Current values as a record mapping field IDs to their values */
  value?: Record<string, string>;
  /** Callback function called when any field value changes */
  onChange: (value: Record<string, string>) => void;
  /** Whether the entire form is required (shows asterisk indicator) */
  required?: boolean;
  /** Custom label for the required indicator */
  requiredLabel?: string;
  /** Error message to display */
  errorMessage?: string;
  /**
   * The failing fields, by field id, each with its own message. When given, only these inputs are marked invalid and each is described by its own message; without it every input is marked invalid.
   */
  fieldErrors?: Record<string, string>;
  /** Text direction: 'ltr' (left-to-right), 'rtl' (right-to-left), or 'auto' (auto-detect from content) */
  dir?: "ltr" | "rtl" | "auto";
  /** Whether the controls are disabled */
  disabled?: boolean;
  /** Image URL to display above the headline */
  imageUrl?: string;
  /** Video URL to display above the headline */
  videoUrl?: string;
}

function FormField({
  elementId,
  headline,
  description,
  fields,
  value = {},
  onChange,
  required = false,
  requiredLabel,
  errorMessage,
  fieldErrors,
  dir = "auto",
  disabled = false,
  imageUrl,
  videoUrl,
}: Readonly<FormFieldProps>): React.JSX.Element {
  // `value` defaults to {} in the destructuring above, so it is always an object here.
  const currentValues = value;

  // Determine if a field is required
  const isFieldRequired = (field: FormFieldConfig): boolean => {
    if (field.required) {
      return true;
    }

    // If all fields are optional and the form is required, then fields should be required
    const visibleFields = fields.filter((f) => f.show !== false);
    const allOptional = visibleFields.every((f) => !f.required);
    if (allOptional && required) {
      return true;
    }

    return false;
  };

  // Handle field value change
  const handleFieldChange = (fieldId: string, fieldValue: string): void => {
    onChange({
      ...currentValues,
      [fieldId]: fieldValue,
    });
  };

  // Get visible fields
  const visibleFields = fields.filter((field) => field.show !== false);

  // This element has no single `inputId` — its inputs are keyed off `elementId` — so the error
  // region id follows the same `${elementId}-...` scheme the field ids already use.
  //
  // The element-level message is deliberately NOT referenced by the individual inputs via aria-describedby: it is one already-flattened message for the whole element, field-scoped for contact-info and address ("Email: ..."), so pointing every input at it would describe "First name" with the email's problem. With `fieldErrors`, each failing input is marked invalid and described by its own message instead, and the other inputs are left alone; without it, every input is marked invalid as before.
  const errorAria = getElementErrorAria(elementId, errorMessage);
  const hasFieldErrors = Boolean(errorMessage) && Object.keys(fieldErrors ?? {}).length > 0;

  return (
    <div className="w-full space-y-4" id={elementId} dir={dir}>
      {/* Headline */}
      <ElementHeader
        headline={headline}
        description={description}
        required={required}
        requiredLabel={requiredLabel}
        imageUrl={imageUrl}
        videoUrl={videoUrl}
      />

      {/* Form Fields */}
      <div className="relative" data-element-input>
        <ElementError errorMessage={errorMessage} dir={dir} id={errorAria.errorId} />
        <div className="space-y-3">
          {visibleFields.map((field) => {
            const fieldRequired = isFieldRequired(field);
            const fieldValue = currentValues[field.id] ?? "";
            const fieldInputId = `${elementId}-${field.id}`;

            // Determine input type
            let inputType: "text" | "email" | "tel" | "number" | "url" = field.type ?? "text";
            if (field.id === "email" && !field.type) {
              inputType = "email";
            } else if (field.id === "phone" && !field.type) {
              inputType = "tel";
            }

            // An explicit token wins; otherwise the built-in field ids carry a known subject.
            const autocomplete = field.autocomplete ?? getFormFieldAutocomplete(field.id);
            const fieldError = hasFieldErrors ? fieldErrors?.[field.id] : undefined;
            const fieldErrorId = `${fieldInputId}-error`;
            const isFieldInvalid = hasFieldErrors ? Boolean(fieldError) : errorAria.ariaInvalid;

            return (
              <div key={field.id} className="space-y-2">
                <Label htmlFor={fieldInputId} variant="default">
                  {fieldRequired ? `${field.label}*` : field.label}
                </Label>
                <Input
                  id={fieldInputId}
                  type={inputType}
                  value={fieldValue}
                  onChange={(e) => {
                    handleFieldChange(field.id, e.target.value);
                  }}
                  required={fieldRequired}
                  disabled={disabled}
                  dir={dir}
                  autoComplete={autocomplete}
                  aria-invalid={isFieldInvalid || undefined}
                  aria-describedby={fieldError ? fieldErrorId : undefined}
                />
                {fieldError ? (
                  <p id={fieldErrorId} className="text-destructive text-sm" dir={dir}>
                    {fieldError}
                  </p>
                ) : null}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

export { FormField };
export type { FormFieldProps };
