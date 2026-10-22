import { expect, test } from "vite-plus/test";
import { boldRuns } from "./rich-text";

test("splits the bold markup out of a message", () => {
  expect(boldRuns("Built on the <b>fastest growing</b> platform. <b>Self-host</b>")).toEqual([
    { text: "Built on the ", bold: false },
    { text: "fastest growing", bold: true },
    { text: " platform. ", bold: false },
    { text: "Self-host", bold: true },
  ]);
});

test("anything but the bold markup stays text", () => {
  expect(boldRuns("plain")).toEqual([{ text: "plain", bold: false }]);
  expect(boldRuns("<i>x</i> <b>open")).toEqual([{ text: "<i>x</i> <b>open", bold: false }]);
  expect(boldRuns("")).toEqual([]);
});
