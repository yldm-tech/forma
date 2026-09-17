import { describe, expect, test } from "vitest";
import { CONTACTS_API_V1_NOT_ENABLED_MESSAGE } from "./contacts-entitlement";

describe("contacts entitlement messages", () => {
  test("keeps the v1 wording verbatim, because consumers match on the string", () => {
    expect(CONTACTS_API_V1_NOT_ENABLED_MESSAGE).toBe(
      "Contacts are only enabled for Enterprise Edition, please upgrade."
    );
  });
});
