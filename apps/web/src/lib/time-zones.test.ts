import { expect, test } from "vite-plus/test";
import { ianaTimeZones, organizationSettingsChanges, schedulingTimeZones } from "./time-zones";

test("ianaTimeZones lists the browser's zones without UTC, which is the default entry", () => {
  const zones = ianaTimeZones();
  expect(zones).toContain("Asia/Tokyo");
  expect(zones).not.toContain("UTC");
});

test("ianaTimeZones offers a stored display zone the browser does not list, and UTC only as the default entry", () => {
  const listed = ianaTimeZones();
  const unlisted = ["Asia/Kolkata", "Europe/Kyiv", "Asia/Calcutta"].find((zone) => !listed.includes(zone));
  if (!unlisted) throw new Error("the browser lists every zone this test tries");
  expect(ianaTimeZones(unlisted)).toEqual([...listed, unlisted]);
  expect(ianaTimeZones("Asia/Tokyo")).toEqual(listed);
  expect(ianaTimeZones("UTC")).toEqual(listed);
  expect(ianaTimeZones(null)).toEqual(listed);
});

test("schedulingTimeZones lists the browser's zones with UTC, since the default entry is the instance's zone", () => {
  const zones = schedulingTimeZones();
  expect(zones).toContain("Asia/Tokyo");
  expect(zones.filter((zone) => zone === "UTC")).toHaveLength(1);
});

test("schedulingTimeZones offers a stored zone the browser does not list", () => {
  const listed = schedulingTimeZones();
  const unlisted = ["Asia/Kolkata", "Europe/Kyiv", "Asia/Calcutta"].find((zone) => !listed.includes(zone));
  if (!unlisted) throw new Error("the browser lists every zone this test tries");
  expect(schedulingTimeZones(unlisted)).toEqual([...listed, unlisted]);
  expect(schedulingTimeZones("Asia/Tokyo")).toEqual(listed);
  expect(schedulingTimeZones(null)).toEqual(listed);
});

test("organizationSettingsChanges sends only changed fields this role may change", () => {
  const initial = { name: "Acme", displayTimeZone: null, surveySchedulingTimeZone: null };
  const changed = {
    name: " Acme Inc ",
    displayTimeZone: "Asia/Tokyo",
    surveySchedulingTimeZone: "Asia/Tokyo",
  };
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
