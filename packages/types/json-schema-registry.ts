// The JSON documents the Go server stores, each paired with the zod schema that defines it. Shared by the JSON Schema export and its parity test.
import { z } from "zod";
import { ZOrganizationAccess } from "./api-key";
import { ZSurveyQuotaLogic } from "./quota";
import { ZResponseData, ZResponseMeta, ZResponseTtc, ZResponseVariables } from "./responses";
import { ZSegmentFilters } from "./segment";
import { ZSurveyBlocks } from "./surveys/blocks";
import { ZSurveyFollowUpAction, ZSurveyFollowUpTrigger } from "./surveys/follow-up";
import {
  ZSurveyClosedMessage,
  ZSurveyEndings,
  ZSurveyHiddenFields,
  ZSurveyInlineTriggers,
  ZSurveyMetadata,
  ZSurveyRecaptcha,
  ZSurveySingleUse,
  ZSurveyStyling,
  ZSurveyVariables,
  ZSurveyWelcomeCard,
  ZSurveyWorkspaceOverwrites,
} from "./surveys/types";

export interface JsonSchemaEntry {
  schema: z.ZodType;
  // "input" (the default) validates what clients send, before zod defaults and transforms run. "output" is for schemas whose input side is opaque, such as a `z.unknown().pipe(...)` bounds gate.
  io?: "input" | "output";
}

// File name (without .json) → the zod schema of the column or payload it validates.
export const JSON_SCHEMA_REGISTRY: Record<string, z.ZodType | JsonSchemaEntry> = {
  "survey.welcome_card": ZSurveyWelcomeCard,
  "survey.blocks": ZSurveyBlocks,
  "survey.endings": ZSurveyEndings,
  "survey.hidden_fields": ZSurveyHiddenFields,
  "survey.variables": ZSurveyVariables,
  "survey.styling": ZSurveyStyling,
  "survey.closed_message": ZSurveyClosedMessage,
  "survey.single_use": ZSurveySingleUse,
  "survey.recaptcha": ZSurveyRecaptcha,
  "survey.metadata": ZSurveyMetadata,
  "survey.workspace_overwrites": ZSurveyWorkspaceOverwrites,
  "survey.inline_triggers": ZSurveyInlineTriggers,
  "survey_quota.logic": ZSurveyQuotaLogic,
  "survey_follow_up.trigger": ZSurveyFollowUpTrigger,
  "survey_follow_up.action": ZSurveyFollowUpAction,
  // A bounds gate on `z.unknown()` piped into the recursive filter tree; only the output side has structure.
  "segment.filters": { schema: ZSegmentFilters, io: "output" },
  "response.data": ZResponseData,
  "response.variables": ZResponseVariables,
  "response.ttc": ZResponseTtc,
  "response.meta": ZResponseMeta,
  "api_key.organization_access": ZOrganizationAccess,
};
