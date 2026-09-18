import { createInstance } from "i18next";
import { describe, expect, test } from "vitest";
import { buildI18nInitOptions } from "./init";

const RESOURCES = {
  common: { welcome: "Welcome", nested: { deep: "Deep" }, greeting: "Hi {{name}}" },
  s: { footer: "Powered by" },
};

const instanceFor = (language = "en-US") => {
  const instance = createInstance();
  instance.init(buildI18nInitOptions({ language, defaultLanguage: "en-US", resources: RESOURCES }));
  return instance;
};

describe("buildI18nInitOptions", () => {
  // The whole point of these options: no await, so the first render — server or client — is already
  // translated rather than gated on a promise.
  test("resolves keys without the caller awaiting init", () => {
    expect(instanceFor().t("common.welcome")).toBe("Welcome");
  });

  test("splits the namespace off the first dot and keeps the rest as a nested key path", () => {
    const instance = instanceFor();

    expect(instance.t("common.nested.deep")).toBe("Deep");
    expect(instance.t("s.footer")).toBe("Powered by");
  });

  test("interpolates", () => {
    expect(instanceFor().t("common.greeting", { name: "Evan" })).toBe("Hi Evan");
  });

  test("returns the key for a namespace it was not given, rather than resolving elsewhere", () => {
    // `defaultNS: false` is what buys this: there is no namespace for an unqualified or unknown
    // key to fall into, so a namespace the route did not load is visible instead of silently wrong.
    expect(instanceFor().t("workspace.settings.general.title")).toBe("workspace.settings.general.title");
  });

  test("declares exactly the namespaces it was handed", () => {
    const options = buildI18nInitOptions({
      language: "ja-JP",
      defaultLanguage: "en-US",
      resources: RESOURCES,
    });

    expect(options.ns).toEqual(["common", "s"]);
    expect(options.resources).toEqual({ "ja-JP": RESOURCES });
    expect(options.fallbackLng).toBe("en-US");
  });
});
