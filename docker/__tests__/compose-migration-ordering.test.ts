import { load } from "js-yaml";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, test } from "vitest";

type ComposeService = {
  depends_on?: Record<string, { condition?: string }>;
  environment?: Record<string, string>;
  restart?: string;
};

const compose = load(
  readFileSync(fileURLToPath(new URL("../docker-compose.yml", import.meta.url)), "utf8")
) as { services: Record<string, ComposeService> };

describe("production Compose migration ordering", () => {
  test("the app waits for the one-shot migration job to succeed", () => {
    expect(compose.services.forma.depends_on?.["forma-migrate"]).toEqual({
      condition: "service_completed_successfully",
    });
  });

  test("the app is told not to migrate itself, so the dependency is the only thing sequencing it", () => {
    expect(compose.services.forma.environment?.SKIP_STARTUP_MIGRATION).toBe("true");
    expect(compose.services["forma-migrate"].restart).toBe("no");
  });

  test("startup still does not depend on SpiceDB", () => {
    expect(compose.services.forma.depends_on?.spicedb).toBeUndefined();
  });
});
