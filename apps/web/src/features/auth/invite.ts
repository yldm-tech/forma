import { ApiError } from "../../lib/api";
import type { InviteStatus } from "./api";

/** The outcome of accepting an invite: accepted, or the refusal as the inspection would have named it. */
export type Acceptance = "idle" | "accepted" | Exclude<InviteStatus, "valid">;

/** What the invite page shows. */
export type InvitePageState = Exclude<InviteStatus, "valid"> | "signed_out" | "accepting" | "accepted";

/**
 * The invite page's state from the server's answers, in forma2's InvitePage order: unknown link, expired invite, no session, another account's invite, welcome. The inspection already accounts for the signed-in user, so `valid` with a session means the invite is this user's to accept. Once accepting has an outcome it wins, since the invite no longer inspects as valid afterwards.
 */
export const invitePageState = (
  inspection: InviteStatus,
  signedIn: boolean,
  acceptance: Acceptance
): InvitePageState => {
  if (acceptance !== "idle") return acceptance;
  if (inspection !== "valid") return inspection;
  return signedIn ? "accepting" : "signed_out";
};

/** The accept endpoint's refusals: invite_not_found 404, invite_expired 400, email_does_not_match 403. */
export const acceptanceFailure = (error: unknown): Exclude<Acceptance, "idle" | "accepted"> => {
  if (error instanceof ApiError) {
    if (error.code === "invite_expired") return "expired";
    if (error.code === "email_does_not_match") return "email_mismatch";
  }
  return "not_found";
};
