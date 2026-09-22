import { describe, expect, test, vi } from "vitest";
import { type TResponseData, type TResponseVariables } from "@forma/types/responses";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/constants";
import { type TSurveyOpenTextElement } from "@forma/types/surveys/elements";
import { parseRecallInformation, replaceRecallInfo } from "./recall";

// Mock getLocalizedValue (assuming path and simple behavior)
vi.mock("./i18n", () => ({
  getLocalizedValue: (localizedString: Record<string, string> | undefined, languageCode: string): string => {
    if (!localizedString) return "";
    return localizedString[languageCode] || ""; // Simplified mock: return value for lang or empty string
  },
}));

// Mock date-time functions as they are used internally and we want to isolate recall logic
// The real parser is used on purpose. The previous mock re-implemented it and then read the result
// back with getUTC*, which is exactly the assumption that was wrong in the source: a date-only value
// must name the same calendar day in the viewer's zone, so only the formatter is stubbed here, and
// it reads the local getters the real Intl formatter would.
vi.mock("./date-time", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./date-time")>()),
  formatDateWithOrdinal: vi.fn(
    (date: Date) =>
      `${date.getFullYear()}-${("0" + (date.getMonth() + 1)).slice(-2)}-${("0" + date.getDate()).slice(-2)}_formatted`
  ),
}));

describe("replaceRecallInfo", () => {
  const responseData: TResponseData = {
    name: "John Doe",
    email: "john.doe@example.com",
    age: 30,
    registrationDate: "2023-01-15",
    tags: ["beta", "user"],
    emptyArray: [],
  };

  const variables: TResponseVariables = {
    productName: "Forma",
    userRole: "Admin",
    lastLogin: "2024-03-10",
  };

  test("should replace recall info from responseData", () => {
    const text = "Welcome, #recall:name/fallback:Guest#! Your email is #recall:email/fallback:N/A#.";
    const expected = "Welcome, John Doe! Your email is john.doe@example.com.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should replace recall info from variables if not in responseData", () => {
    const text = "Product: #recall:productName/fallback:N/A#. Role: #recall:userRole/fallback:User#.";
    const expected = "Product: Forma. Role: Admin.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should use fallback if value is not found in responseData or variables", () => {
    const text = "Your organization is #recall:orgName/fallback:DefaultOrg#.";
    const expected = "Your organization is DefaultOrg.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should handle nbsp in fallback", () => {
    const text = "Status: #recall:status/fallback:Pending&nbsp;Review#.";
    const expected = "Status: Pending& ;Review.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should format date strings from responseData", () => {
    const text = "Registered on: #recall:registrationDate/fallback:N/A#.";
    const expected = "Registered on: 2023-01-15_formatted.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should format date strings from variables", () => {
    const text = "Last login: #recall:lastLogin/fallback:N/A#.";
    const expected = "Last login: 2024-03-10_formatted.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should pass the selected survey language to date formatting", async () => {
    const { formatDateWithOrdinal } = await import("./date-time");
    const text = "Registered on: #recall:registrationDate/fallback:N/A#.";

    replaceRecallInfo(text, responseData, variables, "fr-FR");

    expect(vi.mocked(formatDateWithOrdinal)).toHaveBeenCalledWith(expect.any(Date), "fr-FR");
  });

  test("should join array values with a comma and space", () => {
    const text = "Tags: #recall:tags/fallback:none#.";
    const expected = "Tags: beta, user.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should handle empty array values, replacing with fallback", () => {
    const text = "Categories: #recall:emptyArray/fallback:No&nbsp;Categories#.";
    const expected = "Categories: No& ;Categories.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should handle multiple recall patterns in a single string", () => {
    const text =
      "Hi #recall:name/fallback:User#, welcome to #recall:productName/fallback:Our Product#. Your role is #recall:userRole/fallback:Member#.";
    const expected = "Hi John Doe, welcome to Forma. Your role is Admin.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should return original text if no recall pattern is found", () => {
    const text = "This is a normal text without recall info.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(text);
  });

  test("should handle recall ID not found, using fallback", () => {
    const text = "Value: #recall:nonExistent/fallback:FallbackValue#.";
    const expected = "Value: FallbackValue.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should handle if recall info is incomplete (e.g. missing fallback part), effectively using empty fallback", () => {
    // This specific pattern is not fully matched by extractRecallInfo, leading to no replacement.
    // The current extractRecallInfo expects #recall:ID/fallback:VALUE#
    const text = "Test: #recall:name#";
    const expected = "Test: #recall:name#"; // No change as pattern is not fully matched by extractRecallInfo
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should handle complex fallback with spaces and special characters encoded as nbsp", () => {
    const text =
      "Details: #recall:extraInfo/fallback:Value&nbsp;With&nbsp;Spaces# and #recall:anotherInfo/fallback:Default#";
    const expected = "Details: Value& ;With& ;Spaces and Default";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should handle fallback with only 'nbsp'", () => {
    const text = "Note: #recall:note/fallback:nbsp#.";
    const expected = "Note: .";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("should handle fallback with only '&nbsp;'", () => {
    const text = "Note: #recall:note/fallback:&nbsp;#.";
    const expected = "Note: & ;.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });

  test("returns when a recalled value is itself a recall token", () => {
    // A hidden field can arrive as ?name=%23recall%3Aname%2Ffallback%3Ax%23, and a respondent can type
    // the same string into an open text question a later headline recalls. Substituting must not be
    // re-entrant: rescanning the whole text finds the re-emitted token forever and locks up the tab.
    const selfReferential: TResponseData = { name: "#recall:name/fallback:x#" };
    expect(replaceRecallInfo("Hi #recall:name/fallback:friend#!", selfReferential, {})).toBe(
      "Hi #recall:name/fallback:x#!"
    );
  });

  test("keeps substituting the tokens after a self-referential one", () => {
    const selfReferential: TResponseData = { greeting: "#recall:role/fallback:x#", role: "Admin" };
    const text = "#recall:greeting/fallback:Hi# — you are #recall:role/fallback:Member#.";
    expect(replaceRecallInfo(text, selfReferential, {})).toBe("#recall:role/fallback:x# — you are Admin.");
  });

  test("inserts a recalled value containing '$&' literally", () => {
    // `String.prototype.replace` would expand `$&` into the token it just matched, re-emitting a
    // recall token from a plain answer.
    const withDollar: TResponseData = { code: "50$&OFF" };
    expect(replaceRecallInfo("Use #recall:code/fallback:none#.", withDollar, {})).toBe("Use 50$&OFF.");
  });

  test("should handle fallback with '$nbsp;' (should not replace '$nbsp;')", () => {
    const text = "Note: #recall:note/fallback:$nbsp;#.";
    const expected = "Note: $ ;.";
    expect(replaceRecallInfo(text, responseData, variables)).toBe(expected);
  });
});

describe("parseRecallInformation", () => {
  // Re-use responseData and variables from the outer scope
  const responseData: TResponseData = {
    name: "John Doe",
    email: "john.doe@example.com",
    age: 30,
    registrationDate: "2023-01-15",
    tags: ["beta", "user"],
    emptyArray: [],
    city: "Testville",
  };

  const variables: TResponseVariables = {
    productName: "Forma",
    userRole: "Admin",
    lastLogin: "2024-03-10",
    surveyType: "Onboarding",
  };

  const baseQuestion: TSurveyOpenTextElement = {
    id: "survey1",
    type: TSurveyElementTypeEnum.OpenText,
    headline: { en: "Original Headline" },
    required: false,
    inputType: "text",
    charLimit: { enabled: false },
  };

  test("should replace recall info in headline", () => {
    const question: TSurveyOpenTextElement = {
      ...baseQuestion,
      headline: { en: "Welcome, #recall:name/fallback:Guest#!" },
    };
    const expectedHeadline = "Welcome, John Doe!";
    const result = parseRecallInformation(question, "en", responseData, variables);
    expect(result.headline.en).toBe(expectedHeadline);
  });

  test("does not throw when the language code is not a content key", () => {
    // After canonicalization, content is keyed "hi-IN"/"default"; an SDK may still request legacy "hi".
    const question: TSurveyOpenTextElement = {
      ...baseQuestion,
      headline: { default: "Welcome!", "hi-IN": "स्वागत है" },
      subheader: { default: "Subtitle", "hi-IN": "उपशीर्षक" },
    };
    expect(() => parseRecallInformation(question, "hi", responseData, variables)).not.toThrow();
  });

  test("should replace recall info in subheader", () => {
    const question: TSurveyOpenTextElement = {
      ...baseQuestion,
      headline: { en: "Main Question" },
      subheader: { en: "Details: #recall:productName/fallback:N/A#." },
    };
    const expectedSubheader = "Details: Forma.";
    const result = parseRecallInformation(question, "en", responseData, variables);
    expect(result.subheader?.en).toBe(expectedSubheader);
  });

  test("should replace recall info in both headline and subheader", () => {
    const question: TSurveyOpenTextElement = {
      ...baseQuestion,
      headline: { en: "User: #recall:name/fallback:User#" },
      subheader: { en: "Survey: #recall:surveyType/fallback:General#" },
    };
    const result = parseRecallInformation(question, "en", responseData, variables);
    expect(result.headline.en).toBe("User: John Doe");
    expect(result.subheader?.en).toBe("Survey: Onboarding");
  });

  test("should not change text if no recall info is present", () => {
    const question: TSurveyOpenTextElement = {
      ...baseQuestion,
      headline: { en: "A simple question." },
      subheader: { en: "With a simple subheader." },
    };
    const result = parseRecallInformation(
      JSON.parse(JSON.stringify(question)),
      "en",
      responseData,
      variables
    );
    expect(result.headline.en).toBe(question.headline.en);
    expect(result.subheader?.en).toBe(question.subheader?.en);
  });

  test("should handle undefined subheader gracefully", () => {
    const question: TSurveyOpenTextElement = {
      ...baseQuestion,
      headline: { en: "Question with #recall:name/fallback:User#" },
      subheader: undefined,
    };
    const result = parseRecallInformation(question, "en", responseData, variables);
    expect(result.headline.en).toBe("Question with John Doe");
    expect(result.subheader).toBeUndefined();
  });

  test("should not modify subheader if languageCode content is missing, even if recall is in other lang", () => {
    const question: TSurveyOpenTextElement = {
      ...baseQuestion,
      headline: { en: "Hello #recall:name/fallback:User#" },
      subheader: { fr: "Bonjour #recall:name/fallback:Utilisateur#", en: "" },
    };
    const result = parseRecallInformation(question, "en", responseData, variables);
    expect(result.headline.en).toBe("Hello John Doe");
    expect(result.subheader?.en).toBe("");
    expect(result.subheader?.fr).toBe("Bonjour #recall:name/fallback:Utilisateur#");
  });

  test("should handle malformed recall string (empty ID) leading to no replacement for that pattern", () => {
    // This tests extractId returning null because extractRecallInfo won't match '#recall:/fallback:foo#'
    // due to idPattern requiring at least one char for ID.
    const question: TSurveyOpenTextElement = {
      ...baseQuestion,
      headline: { en: "Malformed: #recall:/fallback:foo# and valid: #recall:name/fallback:User#" },
    };
    const result = parseRecallInformation(question, "en", responseData, variables);
    expect(result.headline.en).toBe("Malformed: #recall:/fallback:foo# and valid: John Doe");
  });

  test("should use empty string for empty fallback value", () => {
    // This tests extractFallbackValue returning ""
    const question: TSurveyOpenTextElement = {
      ...baseQuestion,
      headline: { en: "Data: #recall:nonExistentData/fallback:#" },
    };
    const result = parseRecallInformation(question, "en", responseData, variables);
    expect(result.headline.en).toBe("Data: "); // nonExistentData not found, empty fallback used
  });

  test("returns the input untouched when neither headline nor subheader carries a recall token", () => {
    const question: TSurveyOpenTextElement = {
      ...baseQuestion,
      headline: { en: "A simple question." },
      subheader: { en: "With a simple subheader." },
    };
    const result = parseRecallInformation(question, "en", responseData, variables);
    // Identity, not just equality: the deep clone is skipped when there is nothing to substitute.
    expect(result).toBe(question);
    expect(result.headline.en).toBe("A simple question.");
    expect(result.subheader?.en).toBe("With a simple subheader.");
  });

  test("clones rather than mutating the input when a recall token is present", () => {
    const question: TSurveyOpenTextElement = {
      ...baseQuestion,
      headline: { en: "Welcome, #recall:name/fallback:Guest#!" },
      subheader: { en: "Role: #recall:userRole/fallback:None#" },
    };
    const result = parseRecallInformation(question, "en", responseData, variables);
    expect(result).not.toBe(question);
    expect(result.headline.en).toBe("Welcome, John Doe!");
    expect(result.subheader?.en).toBe("Role: Admin");
    expect(question.headline.en).toBe("Welcome, #recall:name/fallback:Guest#!");
    expect(question.subheader?.en).toBe("Role: #recall:userRole/fallback:None#");
  });

  test("should handle recall info if subheader is present but no text for languageCode", () => {
    const question: TSurveyOpenTextElement = {
      ...baseQuestion,
      headline: { en: "Headline #recall:name/fallback:User#" },
      subheader: { fr: "French subheader #recall:productName/fallback:Produit#", en: "" },
    };
    const result = parseRecallInformation(question, "en", responseData, variables);
    expect(result.headline.en).toBe("Headline John Doe");
    expect(result.subheader?.fr).toBe("French subheader #recall:productName/fallback:Produit#");
    expect(result.subheader?.en).toBe("");
  });
});
