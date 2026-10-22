import { describe, expect, test } from "vite-plus/test";
import { ApiError } from "../../lib/api";
import type { InviteStatus } from "./api";
import { type Acceptance, type InvitePageState, acceptanceFailure, invitePageState } from "./invite";

describe("invitePageState", () => {
  test.each<[InviteStatus, boolean, Acceptance, InvitePageState]>([
    ["not_found", false, "idle", "not_found"],
    ["not_found", true, "idle", "not_found"],
    ["expired", false, "idle", "expired"],
    ["expired", true, "idle", "expired"],
    ["valid", false, "idle", "signed_out"],
    ["email_mismatch", true, "idle", "email_mismatch"],
    ["valid", true, "idle", "accepting"],
    ["valid", true, "accepted", "accepted"],
    ["valid", true, "expired", "expired"],
    ["valid", true, "email_mismatch", "email_mismatch"],
    // After accepting, the invite is gone and inspects as not found; the outcome still stands.
    ["not_found", true, "accepted", "accepted"],
  ])("inspection %s, signed in %s, acceptance %s: %s", (inspection, signedIn, acceptance, expected) => {
    expect(invitePageState(inspection, signedIn, acceptance)).toBe(expected);
  });
});

describe("acceptanceFailure", () => {
  test.each([
    [new ApiError(404, "invite_not_found", "Invite not found"), "not_found"],
    [new ApiError(400, "invite_expired", "Invite expired"), "expired"],
    [new ApiError(403, "email_does_not_match", "Email does not match"), "email_mismatch"],
    [new ApiError(500, "internal_server_error", "x"), "not_found"],
    [new TypeError("Failed to fetch"), "not_found"],
  ])("%s", (error, expected) => {
    expect(acceptanceFailure(error)).toBe(expected);
  });
});
