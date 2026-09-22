// @vitest-environment happy-dom
import { beforeEach, describe, expect, test, vi } from "vitest";
import { announceToLiveRegion, ensureLiveRegion } from "./live-region";

const LIVE_REGION_ID = "forma-live-region";

describe("ensureLiveRegion", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
  });

  test("creates an accessible visually hidden status region", () => {
    const liveRegion = ensureLiveRegion();

    expect(liveRegion.id).toBe(LIVE_REGION_ID);
    expect(liveRegion.getAttribute("role")).toBe("status");
    expect(liveRegion.getAttribute("aria-live")).toBe("polite");
    expect(liveRegion.getAttribute("aria-atomic")).toBe("true");
    expect(liveRegion.style.position).toBe("absolute");
    expect(document.body.lastElementChild).toBe(liveRegion);
  });

  test("reuses an existing live region", () => {
    const existingRegion = document.createElement("div");
    existingRegion.id = LIVE_REGION_ID;
    existingRegion.textContent = "Existing announcement";
    document.body.appendChild(existingRegion);

    expect(ensureLiveRegion()).toBe(existingRegion);
    expect(document.querySelectorAll(`#${LIVE_REGION_ID}`)).toHaveLength(1);
    expect(existingRegion.textContent).toBe("Existing announcement");
  });
});

describe("announceToLiveRegion", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    vi.useRealTimers();
  });

  test("writes the message into the shared region", async () => {
    announceToLiveRegion("Speed moved to position 1 of 3");

    await new Promise((resolve) => setTimeout(resolve, 1));
    expect(document.getElementById(LIVE_REGION_ID)?.textContent).toBe("Speed moved to position 1 of 3");
  });

  test("clears the region first so a repeated message is announced again", async () => {
    const liveRegion = ensureLiveRegion();
    liveRegion.textContent = "Speed moved to position 1 of 3";

    announceToLiveRegion("Speed moved to position 1 of 3");
    expect(liveRegion.textContent).toBe("");

    await new Promise((resolve) => setTimeout(resolve, 1));
    expect(liveRegion.textContent).toBe("Speed moved to position 1 of 3");
  });

  test("a newer announcement supersedes one still pending", async () => {
    announceToLiveRegion("first");
    announceToLiveRegion("second");

    await new Promise((resolve) => setTimeout(resolve, 1));
    expect(document.getElementById(LIVE_REGION_ID)?.textContent).toBe("second");
  });
});
