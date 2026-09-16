import { z } from "zod";
import { ZId } from "@forma/types/common";
import { ZSurveyFilters, ZSurveyStatus, ZSurveyType } from "@forma/types/surveys/types";

/**
 * Every schema here is `.strict()`, so an argument a tool does not declare is a loud error instead of a
 * silently discarded one (ENG-2256).
 *
 * This has teeth only since the SDK v2 migration. v1 was handed a raw `.shape` and rebuilt it via
 * `objectFromShape()`, which dropped any unknown-keys policy — so `.strict()` here would have been
 * decorative and a misspelled argument was stripped before our code ran. On a *filter* that failed in
 * the dangerous direction: `count_feedback_records` with the pre-#8650 spelling `userId` returned the
 * count for every record in the dataset rather than that user's, and reported success, so the agent had
 * no signal it had been handed the wrong number. v2 validates the schema instance itself, so strictness
 * is now enforced where it is declared.
 *
 * The cost, accepted deliberately: `additionalProperties: false` is advertised on every tool, so a
 * client that adds its own keys to `arguments` is rejected rather than tolerated.
 *
 * **Structured sub-objects are strict too, not just the outer one.** `.strict()` binds a single object,
 * so `filter` and its `name`/`status`/`type` children each need it as well — otherwise the same bug
 * reopens one level down, and worse, quietly: a misspelled `filter.status.include` is dropped, leaves
 * `filter.status` as `{}`, and the query runs unfiltered while reporting success. Raised in review on
 * #8859 after the first version of this change only did the outer objects.
 *
 * What still accepts an arbitrary nested shape, deliberately: the free-form `z.record` fields (`blocks`,
 * `metadata`, `welcomeCard`, and the `data` payloads), which the v3 survey document contract validates
 * once the call reaches the operation.
 *
 * What still accepts one *undeliberately*: everything below `definition` on the two workflow tools. That
 * subtree is `ZWorkflowDefinition` from `packages/workflows`, shared with the v3 Workflows REST route and
 * the builder, so it is not this layer's to tighten — see the note in `./workflow-schemas.ts` (ENG-2437).
 * It is the one remaining hole, and `./schemas.test.ts` holds it there: every other structured object in
 * every MCP tool schema, at any depth, must be strict or the suite fails.
 *
 * Adding a schema? Add `.strict()` with it — and to every structured object nested inside it. Prefer
 * `z.strictObject({...})` for the nested ones: `.strict()` returns a clone that drops `.describe()`, so
 * appending it after a `.describe()` silently deletes the description the model reads.
 *
 *
 * And do not turn on @posthog/mcp's `context` injection (`lib/posthog/mcp-tracing.ts` keeps it off): it
 * injects a `context` argument into every tool's advertised schema, which these schemas would reject.
 */

export const ZMcpListSurveysInput = z
  .object({
    workspaceId: ZId.describe("Workspace ID whose surveys should be listed."),
    // Deliberately capped below the HTTP API's 250: for an agent client the binding constraint is
    // context window, not server cost, so a larger page is a cost rather than a convenience.
    limit: z
      .number()
      .int()
      .min(1)
      .max(100)
      .describe("Maximum number of surveys to return. Defaults to 20.")
      .default(20),
    cursor: z
      .string()
      .min(1)
      .optional()
      .describe("Opaque pagination cursor from a previous list_surveys response."),
    includeTotalCount: z
      .boolean()
      .describe(
        "Whether to include the total matching survey count in the response metadata. Defaults to true."
      )
      .default(true),
    filter: z
      .object({
        name: z
          .strictObject({
            contains: z.string().max(512).optional().describe("Case-insensitive survey name substring."),
          })
          .describe("Filter by survey name.")
          .optional(),
        status: z
          .strictObject({
            in: z
              .array(ZSurveyStatus)
              .optional()
              .describe("Survey statuses to include, for example draft or inProgress."),
          })
          .describe("Filter by survey status.")
          .optional(),
        type: z
          .strictObject({
            in: z.array(ZSurveyType).optional().describe("Survey types to include, for example link."),
          })
          .describe("Filter by survey type.")
          .optional(),
      })
      .strict()
      .describe("Optional supported v3 survey filters.")
      .optional(),
    sortBy: ZSurveyFilters.shape.sortBy
      .optional()
      .describe("Sort field for pagination. Defaults to the v3 API default of updatedAt."),
  })
  .strict();

export const ZMcpGetSurveyInput = z
  .object({
    surveyId: z.cuid2().describe("Survey ID to fetch."),
    lang: z
      .array(z.string().trim().min(1))
      .optional()
      .describe("Optional language codes or configured aliases used to filter translatable survey fields."),
  })
  .strict();

const ZMcpSurveyLanguageInput = z.strictObject({
  code: z.string().trim().min(1).describe("Language code or configured language alias."),
  default: z.boolean().optional().describe("Whether this language is the default language."),
  enabled: z.boolean().optional().describe("Whether this language is enabled."),
});

const ZMcpObjectInput = z.record(z.string(), z.unknown());

export const ZMcpCreateSurveyInput = z
  .object({
    workspaceId: ZId.describe("Workspace ID where the survey should be created."),
    name: z.string().trim().min(1).describe("Survey name."),
    type: z.literal("link").optional().describe("Survey type. Only link surveys are supported."),
    status: ZSurveyStatus.optional().describe("Initial survey status. Defaults to draft."),
    defaultLanguage: z
      .string()
      .trim()
      .min(1)
      .optional()
      .describe("Default language code or configured language alias. Defaults to en-US."),
    metadata: ZMcpObjectInput.optional().describe("Survey metadata using the v3 survey document contract."),
    languages: z
      .array(ZMcpSurveyLanguageInput)
      .optional()
      .describe("Configured survey languages using the v3 survey document contract."),
    welcomeCard: ZMcpObjectInput.optional().describe("Welcome card using the v3 survey document contract."),
    blocks: z.array(ZMcpObjectInput).min(1).describe("Survey blocks using the v3 survey document contract."),
    endings: z
      .array(ZMcpObjectInput)
      .optional()
      .describe("Survey endings using the v3 survey document contract."),
    hiddenFields: ZMcpObjectInput.optional().describe("Hidden fields using the v3 survey document contract."),
    variables: z
      .array(ZMcpObjectInput)
      .optional()
      .describe("Survey variables using the v3 survey document contract."),
  })
  .strict();

export const ZMcpPatchSurveyInput = z
  .object({
    surveyId: z.cuid2().describe("Survey ID to update."),
    data: z
      .record(z.string(), z.unknown())
      .describe(
        "Strict top-level v3 survey patch payload. Omitted top-level fields are preserved; provided objects and arrays replace that whole subtree."
      ),
  })
  .strict();

export const ZMcpValidateSurveyInput = z
  .object({
    operation: z.enum(["create", "patch"]).describe("Validation operation to run."),
    surveyId: z.cuid2().optional().describe("Survey ID to validate against. Required for patch validation."),
    data: ZMcpObjectInput.describe(
      "Create or patch payload to validate using the v3 survey document contract."
    ),
  })
  .strict();

export const ZMcpDeleteSurveyInput = z
  .object({
    surveyId: z.cuid2().describe("Survey ID to delete."),
  })
  .strict();

// list_workspaces takes no arguments — it returns the workspaces the authenticated caller can access.
//
// The one schema where `.strict()` buys no ENG-2256 protection: with no declared keys there is nothing to
// misspell, so this is uniformity rather than a fix. Kept strict anyway, as a deliberate call rather than an
// oversight, but the asymmetry is worth knowing if a client ever trips on it. It advertises
// `{"type":"object","properties":{},"additionalProperties":false}`, and a client that pads a zero-argument
// call with a placeholder key would now fail here where v1 dropped it — costly out of proportion, because
// this is the discovery tool every workspace-scoped tool takes its `workspaceId` from, so losing it looks
// like the whole server being broken. Raised in review on #8859; no such client is confirmed, and our own
// QA only exercises Claude Code, so treat a report of "the Forma MCP server won't connect" from
// another client as a reason to look here first.
export const ZMcpListWorkspacesInput = z.object({}).strict();

export type TMcpListSurveysInput = z.infer<typeof ZMcpListSurveysInput>;
export type TMcpListWorkspacesInput = z.infer<typeof ZMcpListWorkspacesInput>;
export type TMcpGetSurveyInput = z.infer<typeof ZMcpGetSurveyInput>;
export type TMcpCreateSurveyInput = z.infer<typeof ZMcpCreateSurveyInput>;
export type TMcpPatchSurveyInput = z.infer<typeof ZMcpPatchSurveyInput>;
export type TMcpValidateSurveyInput = z.infer<typeof ZMcpValidateSurveyInput>;
export type TMcpDeleteSurveyInput = z.infer<typeof ZMcpDeleteSurveyInput>;
