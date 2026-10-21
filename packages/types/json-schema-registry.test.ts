import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, test } from "vite-plus/test";
import { z } from "zod";
import { JSON_SCHEMA_REGISTRY } from "./json-schema-registry";

interface ParityCase {
  name: string;
  schema: string;
  zod: boolean;
  jsonSchema: boolean;
  document: unknown;
}

// The same fixtures drive the Go test in server/internal/jsonschema, which asserts the `jsonSchema` column against the exported schemas. Together they show what the export keeps and what it cannot.
const cases = JSON.parse(
  readFileSync(resolve(__dirname, "../../server/internal/jsonschema/testdata/parity.json"), "utf8")
) as ParityCase[];

describe("JSON Schema parity fixtures", () => {
  test.each(cases)("$schema: $name", ({ schema, zod, document }) => {
    const entry = JSON_SCHEMA_REGISTRY[schema];
    expect(entry).toBeDefined();
    const zodSchema = entry instanceof z.ZodType ? entry : entry.schema;
    expect(zodSchema.safeParse(document).success).toBe(zod);
  });
});
