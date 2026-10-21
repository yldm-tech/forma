import type { HistoryState } from "@tanstack/react-router";
import { expect, test } from "vite-plus/test";
import { signupEmailFrom, withSignupEmail } from "./history-state";

test("the sign-up address travels in history state and keeps the router's own keys", () => {
  const previous = { key: "abc", __TSR_index: 3 } as unknown as HistoryState;
  const state = withSignupEmail("ada@example.com")(previous);
  expect(state).toMatchObject({ key: "abc", __TSR_index: 3 });
  expect(signupEmailFrom(state)).toBe("ada@example.com");
});

test("an entry without it reads as no address", () => {
  expect(signupEmailFrom(undefined)).toBeUndefined();
  expect(signupEmailFrom({} as HistoryState)).toBeUndefined();
  expect(signupEmailFrom({ signupEmail: 42 } as unknown as HistoryState)).toBeUndefined();
});
