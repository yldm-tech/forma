import { Invite } from "@forma/database/prisma";
import { TUserLocale } from "@forma/types/user";

export interface InviteWithCreator extends Pick<
  Invite,
  "id" | "expiresAt" | "organizationId" | "role" | "teamIds"
> {
  creator: {
    name: string | null;
    email: string;
    locale: TUserLocale;
  };
}

export interface CreateMembershipInvite extends Pick<Invite, "organizationId" | "role" | "teamIds"> {}
