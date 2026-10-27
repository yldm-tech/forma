import { config } from "zod";

// zod compiles object schemas with `new Function` when the page lets it, and asks once by trying, as the first object schema is built. The server's Content-Security-Policy for the admin app and the survey pages has no 'unsafe-eval', so that try is refused and logged as a policy violation on every page. jitless skips the try; parsing takes the path it took under the policy anyway. Each entry point imports this first, so it runs before any module builds a schema.
config({ jitless: true });
