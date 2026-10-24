// Exports the JSON Schema of every JSON document the Go server stores, from the zod schemas that define them, into the Go package that embeds and enforces them.
//
// JSON Schema carries structure only. zod refinements (unique ids, logic that must reference existing questions, and the rest of `surveyRefinement`) cannot be expressed in it and are dropped here; the Go service layer ports those rules as code.
//
// Usage: `vp run --filter @forma/types export-json-schema` to write, `... -- --check` to fail when the committed files are stale.
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { z } from "zod";
import { JSON_SCHEMA_REGISTRY, type JsonSchemaEntry } from "../json-schema-registry";

const OUT_DIR = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../../apps/server/internal/jsonschema/schemas"
);

const CONSTRAINING_KEYWORDS = ["type", "anyOf", "oneOf", "allOf", "$ref", "enum", "const"];

const render = (name: string, entry: z.ZodType | JsonSchemaEntry): string => {
  const { schema, io = "input" } = entry instanceof z.ZodType ? { schema: entry } : entry;
  const json = z.toJSONSchema(schema, {
    target: "draft-2020-12",
    io,
    // Dates and other values JSON cannot carry become unconstrained; the service layer checks them.
    unrepresentable: "any",
    cycles: "ref",
    reused: "ref",
    // zod's email pattern uses lookaheads, which Go's RE2 engine rejects. The Go validator asserts `format: "email"` instead.
    override: ({ jsonSchema }) => {
      if (jsonSchema.format === "email") delete jsonSchema.pattern;
    },
  });
  // A schema zod could not represent comes out as `{}`, which accepts anything. Fail instead of shipping a validator that validates nothing.
  if (!CONSTRAINING_KEYWORDS.some((keyword) => keyword in json)) {
    throw new Error(
      `${name}: exported JSON Schema has no constraints; pick a representable schema or io side`
    );
  }
  return `${JSON.stringify({ ...json, $id: `https://forma.yldm.ai/schemas/${name}.json` }, null, 2)}\n`;
};

const main = async (): Promise<void> => {
  const check = process.argv.includes("--check");
  await mkdir(OUT_DIR, { recursive: true });
  const stale: string[] = [];
  for (const [name, entry] of Object.entries(JSON_SCHEMA_REGISTRY)) {
    const path = join(OUT_DIR, `${name}.json`);
    const next = render(name, entry);
    if (check) {
      const current = await readFile(path, "utf8").catch(() => "");
      if (current !== next) stale.push(path);
    } else {
      await writeFile(path, next);
    }
  }
  if (stale.length > 0) {
    console.error(`JSON Schemas are out of date; run the export without --check:\n${stale.join("\n")}`);
    process.exit(1);
  }
  console.log(
    `${check ? "Checked" : "Wrote"} ${Object.keys(JSON_SCHEMA_REGISTRY).length} schemas in ${OUT_DIR}`
  );
};

await main();
