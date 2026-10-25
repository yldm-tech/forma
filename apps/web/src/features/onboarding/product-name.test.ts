import { describe, expect, test } from "vite-plus/test";
import { companyNameFromEmail, initialProductName } from "./product-name";

describe("companyNameFromEmail", () => {
  test.each([
    ["ada@acme.com", "Acme"],
    ["ada@mail.acme.com", "Acme"],
    ["ada@acme.co.jp", "Acme"],
    ["ada@sales.acme.com.au", "Acme"],
    ["ada@forma.yldm.ai", "Yldm"],
    ["Ada@ACME.io.", "Acme"],
  ])("suggests the registrable label of %s", (email, want) => {
    expect(companyNameFromEmail(email)).toBe(want);
  });

  test.each([
    "ada@gmail.com",
    "ada@yahoo.co.jp",
    "ada@docomo.ne.jp",
    "ada@icloud.com",
    "ada@localhost",
    "not-an-address",
    "@acme.com",
    "ada@xn--80ak6aa92e.com",
  ])("suggests nothing for %s", (email) => {
    expect(companyNameFromEmail(email)).toBe("");
  });
});

describe("initialProductName", () => {
  test("keeps a workspace name someone chose", () => {
    expect(initialProductName("Acme Mobile", "ada@gmail.com")).toBe("Acme Mobile");
  });

  test("replaces the default workspace name with what the address suggests", () => {
    expect(initialProductName("My workspace", "ada@acme.com")).toBe("Acme");
    expect(initialProductName("My workspace", "ada@gmail.com")).toBe("");
  });
});
