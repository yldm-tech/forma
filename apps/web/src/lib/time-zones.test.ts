import { expect, test } from "vite-plus/test";
import { ianaTimeZones, organizationSettingsChanges, schedulingTimeZones } from "./time-zones";

test("ianaTimeZones lists the browser's zones without UTC, which is the default entry", () => {
  const zones = ianaTimeZones();
  expect(zones).toContain("Asia/Tokyo");
  expect(zones).not.toContain("UTC");
});

test("schedulingTimeZones lists the browser's zones with UTC, since the default entry is the instance's zone", () => {
  const zones = schedulingTimeZones();
  expect(zones).toContain("Asia/Tokyo");
  expect(zones.filter((zone) => zone === "UTC")).toHaveLength(1);
});

test("organizationSettingsChanges sends only changed fields this role may change", () => {
  const initial = { name: "Acme", displayTimeZone: null, surveySchedulingTimeZone: null };
  const changed = { name: " Acme Inc ", displayTimeZone: "Asia/Tokyo", surveySchedulingTimeZone: "Asia/Tokyo" };
  expect(organizationSettingsChanges(initial, changed, "owner")).toEqual({
    name: "Acme Inc",
    displayTimeZone: "Asia/Tokyo",
    surveySchedulingTimeZone: "Asia/Tokyo",
  });
  expect(organizationSettingsChanges(initial, changed, "manager")).toEqual({
    displayTimeZone: "Asia/Tokyo",
    surveySchedulingTimeZone: "Asia/Tokyo",
  });
  expect(organizationSettingsChanges(initial, changed, "member")).toBeNull();
  expect(
    organizationSettingsChanges(
      initial,
      { name: "Acme ", displayTimeZone: null, surveySchedulingTimeZone: null },
      "owner"
    )
  ).toBeNull();
  expect(
    organizationSettingsChanges(
      { name: "Acme", displayTimeZone: "Asia/Tokyo", surveySchedulingTimeZone: "Asia/Tokyo" },
      { name: "Acme", displayTimeZone: null, surveySchedulingTimeZone: "Asia/Tokyo" },
      "owner"
    )
  ).toEqual({ displayTimeZone: null });
  expect(
    organizationSettingsChanges(
      { name: "Acme", displayTimeZone: null, surveySchedulingTimeZone: "Asia/Tokyo" },
      { name: "Acme", displayTimeZone: null, surveySchedulingTimeZone: null },
      "manager"
    )
  ).toEqual({ surveySchedulingTimeZone: null });
});
