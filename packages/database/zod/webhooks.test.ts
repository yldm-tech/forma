import { describe, expect, test } from "vitest";
import { WebhookSource } from "../src/prisma";
import { ZWebhook } from "./webhooks";

const baseWebhook = {
  id: "clv1abcd2efgh3ijkl4mnop5",
  name: "My webhook",
  createdAt: new Date("2024-01-01T00:00:00.000Z"),
  updatedAt: new Date("2024-01-01T00:00:00.000Z"),
  url: "https://example.com/hook",
  workspaceId: "clv1abcd2efgh3ijkl4mnop6",
  triggers: ["responseFinished"],
  surveyIds: [],
  secret: null,
};

describe("ZWebhook", () => {
  test.each(Object.values(WebhookSource))("accepts every source the database can store (%s)", (source) => {
    expect(ZWebhook.safeParse({ ...baseWebhook, source }).success).toBe(true);
  });

  test("rejects a source the database cannot store", () => {
    expect(ZWebhook.safeParse({ ...baseWebhook, source: "carrier-pigeon" }).success).toBe(false);
  });
});
