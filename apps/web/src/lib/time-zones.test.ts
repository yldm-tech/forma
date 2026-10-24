import { expect, test } from "vite-plus/test";
import { ianaTimeZones, organizationSettingsChanges } from "./time-zones";

test("ianaTimeZones lists the browser's zones without UTC, which is the default entry", () => {
  const zones = ianaTimeZones();
  expect(zones).toContain("Asia/Tokyo");
  expect(zones).not.toContain("UTC");
});

test("organizationSettingsChanges sends only changed fields this role may change", () => {
  const initial = { name: "Acme", displayTimeZone: null };
  const changed = { name: " Acme Inc ", displayTimeZone: "Asia/Tokyo" };
  expect(organizationSettingsChanges(initial, changed, "owner")).toEqual({
    name: "Acme Inc",
    displayTimeZone: "Asia/Tokyo",
  });
  expect(organizationSettingsChanges(initial, changed, "manager")).toEqual({ displayTimeZone: "Asia/Tokyo" });
  expect(organizationSettingsChanges(initial, changed, "member")).toBeNull();
  expect(organizationSettingsChanges(initial, { name: "Acme ", displayTimeZone: null }, "owner")).toBeNull();
  expect(
    organizationSettingsChanges(
      { name: "Acme", displayTimeZone: "Asia/Tokyo" },
      { name: "Acme", displayTimeZone: null },
      "owner"
    )
  ).toEqual({ displayTimeZone: null });
});
