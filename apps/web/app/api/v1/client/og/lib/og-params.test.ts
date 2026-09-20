import { describe, expect, test } from "vitest";
import { OG_NAME_MAX_LENGTH, parseOgParams } from "./og-params";

const parse = (query: string) => parseOgParams(new URLSearchParams(query));

describe("parseOgParams", () => {
  test("caps the name the renderer has to shape", () => {
    const { name } = parse(`name=${"a".repeat(OG_NAME_MAX_LENGTH + 500)}`);

    expect(name).toHaveLength(OG_NAME_MAX_LENGTH);
  });

  test("keeps a name that already fits", () => {
    expect(parse("name=Customer%20satisfaction").name).toBe("Customer satisfaction");
  });

  test("falls back to the default colour for anything that is not a hex literal", () => {
    // The colour used to be concatenated into the style string, so any caller text ended up in the rendered document.
    for (const brandColor of ["red", "javascript:alert(1)", "#12345", "#zzzzzz", ""]) {
      const parsed = parse(`brandColor=${encodeURIComponent(brandColor)}`);

      expect(parsed.brandColor).toBe("#0000BF");
      expect(parsed.backgroundColor).toBe("rgba(0, 0, 191, 0.75)");
    }
  });

  test("renders the requested colour at 75% for every hex form ZColor accepts", () => {
    expect(parse("brandColor=%23ff0000").backgroundColor).toBe("rgba(255, 0, 0, 0.75)");
    // Shorthand and alpha forms broke the old `brandColor + "BF"` concatenation: `#f00BF` is a different colour and `#ff0000ffBF` is not a colour at all.
    expect(parse("brandColor=%23f00").backgroundColor).toBe("rgba(255, 0, 0, 0.75)");
    expect(parse("brandColor=%23f00f").backgroundColor).toBe("rgba(255, 0, 0, 0.75)");
    expect(parse("brandColor=%23ff0000ff").backgroundColor).toBe("rgba(255, 0, 0, 0.75)");
  });

  test("defaults both values when the query string is empty", () => {
    const parsed = parse("");

    expect(parsed).toEqual({
      name: "",
      brandColor: "#0000BF",
      backgroundColor: "rgba(0, 0, 191, 0.75)",
    });
  });
});
