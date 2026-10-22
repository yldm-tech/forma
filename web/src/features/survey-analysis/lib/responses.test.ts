import { describe, expect, test } from "vite-plus/test";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import { extractChoiceIdsFromResponse, getContactIdentifier, processResponseData } from "./responses";

describe("extractChoiceIdsFromResponse", () => {
  const multipleChoiceMultiQuestion = {
    id: "multi-choice-id",
    type: TSurveyElementTypeEnum.MultipleChoiceMulti as typeof TSurveyElementTypeEnum.MultipleChoiceMulti,
    headline: { default: "Select multiple options" },
    required: false,
    choices: [
      {
        id: "choice-1",
        label: { default: "Option 1", es: "Opción 1" },
      },
      {
        id: "choice-2",
        label: { default: "Option 2", es: "Opción 2" },
      },
      {
        id: "choice-3",
        label: { default: "Option 3", es: "Opción 3" },
      },
    ],
    shuffleOption: "none" as const,
  };

  const multipleChoiceSingleQuestion = {
    id: "single-choice-id",
    type: TSurveyElementTypeEnum.MultipleChoiceSingle as typeof TSurveyElementTypeEnum.MultipleChoiceSingle,
    headline: { default: "Select one option" },
    required: false,
    choices: [
      {
        id: "choice-a",
        label: { default: "Choice A", fr: "Choix A" },
      },
      {
        id: "choice-b",
        label: { default: "Choice B", fr: "Choix B" },
      },
    ],
    shuffleOption: "none" as const,
  };

  const textQuestion = {
    id: "text-id",
    type: TSurveyElementTypeEnum.OpenText as typeof TSurveyElementTypeEnum.OpenText,
    headline: { default: "What do you think?" },
    required: false,
    inputType: "text" as const,
    charLimit: { enabled: false, min: 0, max: 0 },
  };

  describe("multipleChoiceMulti questions", () => {
    test("should extract choice IDs from array response with default language", () => {
      const responseValue = ["Option 1", "Option 3"];
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceMultiQuestion, "default");

      expect(result).toEqual(["choice-1", "choice-3"]);
    });

    test("should extract choice IDs from array response with specific language", () => {
      const responseValue = ["Opción 1", "Opción 2"];
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceMultiQuestion, "es");

      expect(result).toEqual(["choice-1", "choice-2"]);
    });

    test("should fall back to checking all language values when exact language match fails", () => {
      const responseValue = ["Opción 1", "Option 2"];
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceMultiQuestion, "default");

      expect(result).toEqual(["choice-1", "choice-2"]);
    });

    test("should render other option when non-matching choice is selected", () => {
      const responseValue = ["Option 1", "Non-existent option", "Option 3"];
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceMultiQuestion, "default");

      expect(result).toEqual(["choice-1", "other", "choice-3"]);
    });

    test("should return empty array for empty response", () => {
      const responseValue: string[] = [];
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceMultiQuestion, "default");

      expect(result).toEqual([]);
    });
  });

  describe("multipleChoiceSingle questions", () => {
    test("should extract choice ID from string response with default language", () => {
      const responseValue = "Choice A";
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceSingleQuestion, "default");

      expect(result).toEqual(["choice-a"]);
    });

    test("should extract choice ID from string response with specific language", () => {
      const responseValue = "Choix B";
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceSingleQuestion, "fr");

      expect(result).toEqual(["choice-b"]);
    });

    test("should fall back to checking all language values for single choice", () => {
      const responseValue = "Choix A";
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceSingleQuestion, "default");

      expect(result).toEqual(["choice-a"]);
    });

    test("should return empty array for empty string response", () => {
      const responseValue = "";
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceSingleQuestion, "default");

      expect(result).toEqual([]);
    });
  });

  describe("edge cases", () => {
    test("should return empty array for non-multiple choice questions", () => {
      const responseValue = "Some text response";
      const result = extractChoiceIdsFromResponse(responseValue, textQuestion, "default");

      expect(result).toEqual([]);
    });

    test("should handle missing language parameter by defaulting to 'default'", () => {
      const responseValue = "Option 1";
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceMultiQuestion);

      expect(result).toEqual(["choice-1"]);
    });

    test("should handle numeric or other types by returning empty array", () => {
      const responseValue = 123;
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceMultiQuestion, "default");

      expect(result).toEqual([]);
    });

    test("should handle object responses by returning empty array", () => {
      const responseValue = { invalid: "object" };
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceMultiQuestion, "default");

      expect(result).toEqual([]);
    });
  });

  describe("language handling", () => {
    test("should use provided language parameter", () => {
      const responseValue = ["Opción 1"];
      const result = extractChoiceIdsFromResponse(responseValue, multipleChoiceMultiQuestion, "es");

      expect(result).toEqual(["choice-1"]);
    });

    test("should handle null language parameter by defaulting to 'default'", () => {
      const responseValue = ["Option 1"];
      const result = extractChoiceIdsFromResponse(
        responseValue,
        multipleChoiceMultiQuestion,
        null as unknown as string
      );

      expect(result).toEqual(["choice-1"]);
    });

    test("should handle undefined language parameter by defaulting to 'default'", () => {
      const responseValue = ["Option 1"];
      const result = extractChoiceIdsFromResponse(
        responseValue,
        multipleChoiceMultiQuestion,
        undefined as unknown as string
      );

      expect(result).toEqual(["choice-1"]);
    });
  });
});

describe("processResponseData", () => {
  test("should handle string input", () => {
    expect(processResponseData("test")).toBe("test");
  });

  test("should handle number input", () => {
    expect(processResponseData(42)).toBe("42");
  });

  test("should handle array input", () => {
    expect(processResponseData(["a", "b", "c"])).toBe("a; b; c");
  });

  test("should filter out empty values from array", () => {
    const input = ["a", "", "c"];
    expect(processResponseData(input)).toBe("a; c");
  });

  test("should handle object input", () => {
    const input = { key1: "value1", key2: "value2" };
    expect(processResponseData(input)).toBe("key1: value1\nkey2: value2");
  });

  test("should filter out empty values from object", () => {
    const input = { key1: "value1", key2: "", key3: "value3" };
    expect(processResponseData(input)).toBe("key1: value1\nkey3: value3");
  });

  test("should return empty string for unsupported types", () => {
    expect(processResponseData(undefined as unknown as string)).toBe("");
  });

  test("should filter out null values from array", () => {
    const input = ["a", null, "c"] as unknown as string[];
    expect(processResponseData(input)).toBe("a; c");
  });

  test("should filter out undefined values from array", () => {
    const input = ["a", undefined, "c"] as unknown as string[];
    expect(processResponseData(input)).toBe("a; c");
  });
});

describe("getContactIdentifier", () => {
  test("prefers the email attribute, then the userId", () => {
    expect(getContactIdentifier({ id: "c1", userId: "u1" }, { email: "a@b.c" })).toBe("a@b.c");
    expect(getContactIdentifier({ id: "c1", userId: "u1" }, {})).toBe("u1");
    expect(getContactIdentifier(null, null)).toBe("");
  });
});
