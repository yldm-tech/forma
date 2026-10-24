import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";

/** A field's rule: the message to show, or undefined when the value passes. It sees every value, for rules that compare fields. */
export type Validator<V, K extends keyof V> = (value: V[K], values: V) => string | undefined;

export type Validators<V> = { [K in keyof V]?: Validator<V, K> };

export type FieldErrors<V> = Partial<Record<keyof V, string>>;

/** Runs every field's rule and keeps the failures. */
export const validateValues = <V extends object>(validators: Validators<V>, values: V): FieldErrors<V> => {
  const errors: FieldErrors<V> = {};
  for (const name of Object.keys(validators) as (keyof V)[]) {
    const message = validators[name]?.(values[name], values);
    if (message) errors[name] = message;
  }
  return errors;
};

export const hasErrors = <V>(errors: FieldErrors<V>): boolean => Object.values(errors).some(Boolean);

/**
 * The errors a form shows, in forma2's react-hook-form mode (validate on submit, then again on every change): rule failures appear only once a submit has been attempted, while an error the server reported for a field shows at once and takes precedence until that field changes.
 */
export const visibleErrors = <V>(
  validation: FieldErrors<V>,
  server: FieldErrors<V>,
  submitted: boolean
): FieldErrors<V> => (submitted ? { ...validation, ...server } : { ...server });

export interface UseFormOptions<V> {
  defaultValues: V;
  /** Must be stable across renders (declare it at module level). */
  validators: Validators<V>;
  onSubmit: (values: V) => Promise<void> | void;
}

export interface FormApi<V> {
  values: V;
  errors: FieldErrors<V>;
  /** A message for the form as a whole, such as a refused request. */
  formError: string | undefined;
  /** Whether every rule passes now, whatever has been shown. */
  isValid: boolean;
  isSubmitting: boolean;
  setValue: <K extends keyof V>(name: K, value: V[K]) => void;
  setFieldError: (name: keyof V, message: string) => void;
  setFormError: (message: string | undefined) => void;
  handleSubmit: (event?: FormEvent) => Promise<void>;
}

/** Form state for the auth screens: values, forma2's validation timing, server-reported errors and the submitting flag. */
export const useForm = <V extends object>({
  defaultValues,
  validators,
  onSubmit,
}: UseFormOptions<V>): FormApi<V> => {
  const [values, setValues] = useState(defaultValues);
  const [submitted, setSubmitted] = useState(false);
  const [serverErrors, setServerErrors] = useState<FieldErrors<V>>({});
  const [formError, setFormError] = useState<string>();
  const [isSubmitting, setSubmitting] = useState(false);

  const validation = useMemo(() => validateValues(validators, values), [validators, values]);
  const errors = useMemo(
    () => visibleErrors(validation, serverErrors, submitted),
    [validation, serverErrors, submitted]
  );
  const isValid = !hasErrors(validation);
  // A refused submit moves focus to the first field it marked invalid, in the order the form shows them, once the errors have rendered. Only a submit does this, never typing.
  const formElement = useRef<HTMLFormElement | null>(null);
  const [focusRequest, setFocusRequest] = useState(0);
  useEffect(() => {
    if (focusRequest === 0) return;
    formElement.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  }, [focusRequest]);

  const setValue = useCallback(<K extends keyof V>(name: K, value: V[K]) => {
    setValues((current) => ({ ...current, [name]: value }));
    setServerErrors((current) => {
      if (!(name in current)) return current;
      const next = { ...current };
      delete next[name];
      return next;
    });
  }, []);

  const setFieldError = useCallback((name: keyof V, message: string) => {
    setServerErrors((current) => ({ ...current, [name]: message }));
  }, []);

  const handleSubmit = async (event?: FormEvent) => {
    event?.preventDefault();
    if (isSubmitting) return;
    if (event?.currentTarget instanceof HTMLFormElement) formElement.current = event.currentTarget;
    setSubmitted(true);
    setFormError(undefined);
    if (!isValid) {
      setFocusRequest((n) => n + 1);
      return;
    }
    setSubmitting(true);
    try {
      await onSubmit(values);
    } finally {
      setSubmitting(false);
    }
  };

  return {
    values,
    errors,
    formError,
    isValid,
    isSubmitting,
    setValue,
    setFieldError,
    setFormError,
    handleSubmit,
  };
};
