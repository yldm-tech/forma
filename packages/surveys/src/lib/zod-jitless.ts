import { config } from "zod";

// zod compiles object schemas with `new Function` when the page lets it, and asks once by trying, as the first object schema is built. Under a Content-Security-Policy without 'unsafe-eval' (Forma's own survey pages and admin app, and many customer sites) that try is refused and logged as a policy violation. jitless skips the try; parsing takes the path it took under such a policy anyway, and a survey's documents are small. index.ts imports this first, so it runs before any module builds a schema.
config({ jitless: true });
