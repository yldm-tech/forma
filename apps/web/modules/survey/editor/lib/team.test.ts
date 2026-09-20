import { beforeEach, describe, expect, test, vi } from "vitest";
import { prisma } from "@forma/database";
import { TFollowUpEmailToUser } from "@/modules/survey/editor/types/survey-follow-up";
import { getTeamMemberDetails } from "./team";

// Mock prisma
vi.mock("@forma/database", () => ({
  prisma: {
    teamUser: {
      findMany: vi.fn(),
    },
    user: {
      findMany: vi.fn(),
    },
  },
}));

describe("getTeamMemberDetails", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  test("should return an empty array if teamIds is empty", async () => {
    const result = await getTeamMemberDetails([]);
    expect(result).toEqual([]);
    expect(prisma.teamUser.findMany).not.toHaveBeenCalled();
    expect(prisma.user.findMany).not.toHaveBeenCalled();
  });

  test("should return unique member details for a single team", async () => {
    const teamId = "team1";
    const mockTeamUsers = [{ userId: "user1" }, { userId: "user2" }];
    const mockUsers: TFollowUpEmailToUser[] = [
      { email: "user1@example.com", name: "User One" },
      { email: "user2@example.com", name: "User Two" },
    ];

    vi.mocked(prisma.teamUser.findMany).mockResolvedValue(Promise.resolve(mockTeamUsers) as any);
    vi.mocked(prisma.user.findMany).mockResolvedValue(Promise.resolve(mockUsers) as any);

    const result = await getTeamMemberDetails([teamId]);

    expect(prisma.teamUser.findMany).toHaveBeenCalledWith({
      where: { teamId: { in: [teamId] } },
      select: { userId: true },
    });
    expect(prisma.user.findMany).toHaveBeenCalledWith({
      where: {
        id: {
          in: ["user1", "user2"],
        },
      },
      select: {
        email: true,
        name: true,
      },
    });
    expect(result).toEqual(mockUsers);
  });

  test("should query every team at once and deduplicate users shared between them", async () => {
    const teamIds = ["team1", "team2"];
    // user1 sits on both teams, so the junction table yields its id twice.
    const mockTeamUsers = [{ userId: "user1" }, { userId: "user1" }, { userId: "user2" }];
    const mockUsers: TFollowUpEmailToUser[] = [
      { email: "user1@example.com", name: "User One" },
      { email: "user2@example.com", name: "User Two" },
    ];

    vi.mocked(prisma.teamUser.findMany).mockResolvedValue(Promise.resolve(mockTeamUsers) as any);
    vi.mocked(prisma.user.findMany).mockResolvedValue(Promise.resolve(mockUsers) as any);

    const result = await getTeamMemberDetails(teamIds);

    // Two queries in total, not two per team.
    expect(prisma.teamUser.findMany).toHaveBeenCalledTimes(1);
    expect(prisma.teamUser.findMany).toHaveBeenCalledWith({
      where: { teamId: { in: teamIds } },
      select: { userId: true },
    });

    expect(prisma.user.findMany).toHaveBeenCalledTimes(1);
    expect(prisma.user.findMany).toHaveBeenCalledWith({
      where: { id: { in: ["user1", "user2"] } },
      select: { email: true, name: true },
    });

    expect(result).toEqual(mockUsers);
    const emails = result.map((r) => r.email);
    expect(new Set(emails).size).toBe(emails.length);
  });

  test("should not query users when the teams have no members", async () => {
    vi.mocked(prisma.teamUser.findMany).mockResolvedValue(Promise.resolve([]) as any);

    const result = await getTeamMemberDetails(["teamWithNoUsers"]);

    expect(prisma.teamUser.findMany).toHaveBeenCalledWith({
      where: { teamId: { in: ["teamWithNoUsers"] } },
      select: { userId: true },
    });
    expect(prisma.user.findMany).not.toHaveBeenCalled();
    expect(result).toEqual([]);
  });

  test("should handle users with null names gracefully", async () => {
    const mockTeamUsers = [{ userId: "user1" }];
    const mockUsers: TFollowUpEmailToUser[] = [{ email: "user1@example.com", name: null as any }]; // Cast to any to satisfy TFollowUpEmailToUser if name is strictly string

    vi.mocked(prisma.teamUser.findMany).mockResolvedValue(Promise.resolve(mockTeamUsers) as any);
    vi.mocked(prisma.user.findMany).mockResolvedValue(Promise.resolve(mockUsers) as any);

    const result = await getTeamMemberDetails(["team1"]);
    expect(result).toEqual([{ email: "user1@example.com", name: null }]);
  });
});
