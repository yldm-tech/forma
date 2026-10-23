import { z } from "zod";
import { ZId, ZUuid } from "./common";
import type { TResponse } from "./responses";
import { ZConnector, ZSingleCondition } from "./surveys/logic";

// Complete quota conditions structure
export const ZSurveyQuotaLogic = z.object({
  connector: ZConnector,
  conditions: z.array(ZSingleCondition),
});
export type TSurveyQuotaLogic = z.infer<typeof ZSurveyQuotaLogic>;

// Survey quota action enum
export const ZSurveyQuotaAction = z.enum(["endSurvey", "continueSurvey"]);
export type TSurveyQuotaAction = z.infer<typeof ZSurveyQuotaAction>;

// Response quota link status enum
export const ZResponseQuotaLinkStatus = z.enum(["screenedIn", "screenedOut"]);
export type TResponseQuotaLinkStatus = z.infer<typeof ZResponseQuotaLinkStatus>;

// The ids of the quota and its survey are UUIDs in the Go server where forma2's were cuid2, and both forms are accepted (v1api.IsID reads z.cuid2() the same way). The ending card id names a card inside the survey document and keeps forma2's cuid2 rule.
const ZQuotaRecordId = z.union([ZId, ZUuid]);

// Survey quota model
export const ZSurveyQuota = z.object({
  id: ZQuotaRecordId,
  createdAt: z.date(),
  updatedAt: z.date(),
  surveyId: ZQuotaRecordId,
  name: z.string().min(1, {
    error: "Quota name is required",
  }),
  limit: z.number().min(1, {
    error: "Limit must be greater than 0",
  }),
  logic: ZSurveyQuotaLogic,
  action: ZSurveyQuotaAction,
  endingCardId: ZId.nullable(),
  countPartialSubmissions: z.boolean(),
});
export type TSurveyQuota = z.infer<typeof ZSurveyQuota>;

export const ZSurveyQuotaInput = ZSurveyQuota.omit({
  id: true,
  createdAt: true,
  updatedAt: true,
}).superRefine((data, ctx) => {
  // Validate ending card when action is endSurvey
  if (data.action === "endSurvey" && (data.endingCardId === null || data.endingCardId === "")) {
    ctx.addIssue({
      code: "custom",
      path: ["action"],
      message: "endingCardId is required when action is endSurvey",
    });
  }
});

export type TSurveyQuotaInput = z.infer<typeof ZSurveyQuotaInput>;

export const ZResponseQuotaLink = z.object({
  responseId: ZId,
  quotaId: ZId,
  status: ZResponseQuotaLinkStatus,
});
export type TResponseQuotaLink = z.infer<typeof ZResponseQuotaLink>;

export interface TQuotaFullEndSurvey {
  action: "endSurvey";
  endingCardId: string;
}

export interface TQuotaFullContinueSurvey {
  action: "continueSurvey";
}

type TQuotaFullAction = TQuotaFullEndSurvey | TQuotaFullContinueSurvey;

export type TQuotaFullResponse = {
  quotaFull: true;
  quotaId: string;
  action: TSurveyQuotaAction;
} & TQuotaFullAction;

export type TResponseWithQuotaFull = TResponse & {
  quotaFull?: TSurveyQuota;
};
