import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { TFollowUpEmailToUser } from "@/modules/survey/editor/types/survey-follow-up";

export const getTeamMemberDetails = reactCache(async (teamIds: string[]): Promise<TFollowUpEmailToUser[]> => {
  if (teamIds.length === 0) {
    return [];
  }

  // Two queries for the whole workspace rather than two per team: this runs on every survey-editor page load, and awaiting a pair of round trips per team made the helper's cost scale with the team count.
  const teamMembers = await prisma.teamUser.findMany({
    where: {
      teamId: {
        in: teamIds,
      },
    },
    select: {
      userId: true,
    },
  });

  // A user on several of these teams appears once per team, so the ids are deduplicated before the lookup. User.email is unique, so the resulting rows are unique too.
  const userIds = Array.from(new Set(teamMembers.map((member) => member.userId)));

  if (userIds.length === 0) {
    return [];
  }

  return prisma.user.findMany({
    where: {
      id: {
        in: userIds,
      },
    },
    select: {
      email: true,
      name: true,
    },
  });
});
