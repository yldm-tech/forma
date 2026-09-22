import { describe, expect, test } from "vitest";
import { getFormFieldAutocomplete, getOpenTextAutocomplete } from "./autocomplete";

// The full set of HTML autofill tokens that may appear in this map. Anything outside it is a typo,
// and a typo is worse than no token: the browser offers the respondent the wrong saved value.
const VALID_AUTOFILL_TOKENS = new Set([
  "given-name",
  "family-name",
  "email",
  "tel",
  "organization",
  "address-line1",
  "address-line2",
  "address-level1",
  "address-level2",
  "postal-code",
  "country-name",
  "url",
]);

describe("getFormFieldAutocomplete", () => {
  test.each([
    ["firstName", "given-name"],
    ["lastName", "family-name"],
    ["email", "email"],
    ["phone", "tel"],
    ["company", "organization"],
    ["addressLine1", "address-line1"],
    ["addressLine2", "address-line2"],
    ["city", "address-level2"],
    ["state", "address-level1"],
    ["zip", "postal-code"],
    ["country", "country-name"],
  ])("maps the built-in field id %s to %s", (fieldId, token) => {
    expect(getFormFieldAutocomplete(fieldId)).toBe(token);
  });

  test("covers every field id of the contact-info and address elements", () => {
    const fieldIds = [
      "firstName",
      "lastName",
      "email",
      "phone",
      "company",
      "addressLine1",
      "addressLine2",
      "city",
      "state",
      "zip",
      "country",
    ];
    const tokens = fieldIds.map((id) => getFormFieldAutocomplete(id));
    expect(tokens.filter(Boolean)).toHaveLength(fieldIds.length);
    for (const token of tokens) {
      expect(VALID_AUTOFILL_TOKENS.has(token as string)).toBe(true);
    }
  });

  test("returns undefined for an id whose subject the renderer cannot know", () => {
    expect(getFormFieldAutocomplete("favouriteColour")).toBeUndefined();
    expect(getFormFieldAutocomplete("")).toBeUndefined();
  });

  test("does not resolve inherited Object properties as tokens", () => {
    expect(getFormFieldAutocomplete("constructor")).toBeUndefined();
    expect(getFormFieldAutocomplete("toString")).toBeUndefined();
  });
});

describe("getOpenTextAutocomplete", () => {
  test("maps the two input types whose subject is the respondent", () => {
    expect(getOpenTextAutocomplete("email")).toBe("email");
    expect(getOpenTextAutocomplete("phone")).toBe("tel");
  });

  test("leaves subject-less input types untokenised", () => {
    // `url` is deliberately absent: the HTML token means the respondent's own home page, which a
    // url-typed survey question usually is not.
    expect(getOpenTextAutocomplete("url")).toBeUndefined();
    expect(getOpenTextAutocomplete("text")).toBeUndefined();
    expect(getOpenTextAutocomplete("number")).toBeUndefined();
  });
});
