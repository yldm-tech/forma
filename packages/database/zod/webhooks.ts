import { z } from "zod";
import { type Webhook, WebhookSource } from "../src/prisma";

export const ZWebhook = z.object({
  id: z.cuid2().describe("The ID of the webhook"),
  name: z.string().nullable().describe("The name of the webhook"),
  createdAt: z
    .date()
    .meta({
      example: "2021-01-01T00:00:00.000Z",
    })
    .describe("The date and time the webhook was created"),
  updatedAt: z
    .date()
    .meta({
      example: "2021-01-01T00:00:00.000Z",
    })
    .describe("The date and time the webhook was last updated"),
  url: z.url().describe("The URL of the webhook"),
  // Derived from the Prisma enum rather than restated, so a new member cannot be missed here: a hand-written mirror silently dropped `activepieces` and made every create request from that integration a 400.
  source: z.enum(WebhookSource).describe("The source of the webhook"),
  workspaceId: z.cuid2().describe("The ID of the workspace"),
  triggers: z
    .array(z.enum(["responseFinished", "responseCreated", "responseUpdated"]))
    .describe("The triggers of the webhook")
    .min(1, {
      error: "At least one trigger is required",
    }),
  surveyIds: z.array(z.cuid2()).describe("The IDs of the surveys "),
  secret: z
    .string()
    .nullable()
    .describe("The shared secret used to generate HMAC signatures for webhook requests"),
}) satisfies z.ZodType<Webhook>;

ZWebhook.meta({
  id: "webhook",
}).describe("A webhook");

// The signing secret is only exposed once, in the create response.
export const ZWebhookWithoutSecret = ZWebhook.omit({ secret: true });

ZWebhookWithoutSecret.meta({
  id: "webhookWithoutSecret",
}).describe("A webhook without its signing secret");
