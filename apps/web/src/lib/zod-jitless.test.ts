import { expect, test } from "vite-plus/test";
import { config } from "zod";
import "./zod-jitless";

// Both entry points import this first, so neither the admin app nor the survey page trips zod's eval probe, which their Content-Security-Policy reports as a violation.
test("zod runs without compiling schemas", () => {
  expect(config().jitless).toBe(true);
});
