/**
 * HTML autofill tokens (WCAG 2.1 SC 1.3.5, "Identify Input Purpose") for the respondent-facing
 * inputs that collect the respondent's *own* contact details or address.
 *
 * A token is only correct when the field is about the person filling the form in — that is what the
 * HTML autofill spec's tokens mean. A wrong token is worse than none, because the browser then
 * offers a saved value that does not belong in the field, so anything whose subject the renderer
 * cannot know (an arbitrary survey question) deliberately gets no token.
 *
 * @see https://html.spec.whatwg.org/multipage/form-control-infrastructure.html#autofill
 */

/**
 * Field ids used by the built-in contact-info and address elements, which compose `FormField`.
 * These ids are fixed by those two elements, so the subject of each field is known.
 */
const FORM_FIELD_AUTOCOMPLETE = new Map<string, string>([
  // contact-info
  ["firstName", "given-name"],
  ["lastName", "family-name"],
  ["email", "email"],
  ["phone", "tel"],
  ["company", "organization"],
  // address
  ["addressLine1", "address-line1"],
  ["addressLine2", "address-line2"],
  ["city", "address-level2"],
  ["state", "address-level1"],
  ["zip", "postal-code"],
  ["country", "country-name"],
]);

/**
 * The autofill token for a `FormField` field id, or `undefined` for an id this map does not know —
 * a future custom field, whose semantics the renderer cannot infer from an author-chosen id. A
 * `Map` rather than an object literal so an author-supplied id can never reach `Object.prototype`.
 */
function getFormFieldAutocomplete(fieldId: string): string | undefined {
  return FORM_FIELD_AUTOCOMPLETE.get(fieldId);
}

/**
 * The autofill token for an open-text element's author-chosen input type.
 *
 * Only `email` and `phone` map. `url` deliberately does not: the HTML `url` token means the
 * respondent's own home page, while a url-typed survey question is as often "link to the article"
 * as "your website" — and `text`/`number` carry no subject at all.
 */
function getOpenTextAutocomplete(
  inputType: "text" | "email" | "url" | "phone" | "number"
): string | undefined {
  if (inputType === "email") return "email";
  if (inputType === "phone") return "tel";
  return undefined;
}

export { getFormFieldAutocomplete, getOpenTextAutocomplete };
