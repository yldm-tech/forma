// Generates the seeded datasets in this directory and the summary forma2's own functions compute for them, which summary_golden_test.go compares the Go summary with. summary.mts is forma2's apps/web/app/(app)/workspaces/[workspaceId]/surveys/[surveyId]/(analysis)/summary/lib/surveySummary.ts at 3db326f, from `interface TSurveySummaryResponse` up to (not including) `export const getSurveySummary`, verbatim, under one line importing its helpers from ./shims.mts. Run from a scratch copy of this directory with Node 24 (`mkdir out && node gen.mts`) and copy out/*.json back.
import { writeFileSync } from "node:fs";
import { getElementSummary, getSurveySummaryDropOff, getSurveySummaryMeta } from "./summary.mts";

let seed = 20261022;
const rand = () => {
  seed = (seed * 1103515245 + 12345) % 2147483648;
  return seed / 2147483648;
};
const pick = (list) => list[Math.floor(rand() * list.length)];
const chance = (p) => rand() < p;
const int = (lo, hi) => lo + Math.floor(rand() * (hi - lo + 1));
const uuid = (n) => `00000000-0000-4000-8000-${String(n).padStart(12, "0")}`;

const choice = (id, def, de) => ({ id, label: de ? { default: def, de } : { default: def } });

const blocks = [
  {
    id: "b1",
    name: "Block 1",
    elements: [
      { id: "q_text", type: "openText", headline: { default: "What do you think?" }, required: false, inputType: "text" },
      {
        id: "q_single",
        type: "multipleChoiceSingle",
        headline: { default: "Favourite colour", de: "Lieblingsfarbe" },
        required: false,
        choices: [choice("c1", "Red", "Rot"), choice("c2", "Blue", "Blau"), choice("other", "Other", "Andere"), choice("none", "None", "Keine")],
      },
      {
        id: "q_multi",
        type: "multipleChoiceMulti",
        headline: { default: "Pick numbers" },
        required: false,
        choices: [choice("m1", "10", "zehn"), choice("m2", "5", "fünf"), choice("m3", "Apple", "Apfel"), choice("m4", "Apple"), choice("other", "")],
      },
    ],
  },
  {
    id: "b2",
    name: "Block 2",
    elements: [
      { id: "q_nps", type: "nps", headline: { default: "Recommend?" }, required: false },
      { id: "q_rating", type: "rating", headline: { default: "Rate" }, required: false, scale: "star", range: 5 },
      { id: "q_csat", type: "csat", headline: { default: "Satisfied?" }, required: false, scale: "smiley", range: 5 },
      { id: "q_ces", type: "ces", headline: { default: "Effort?" }, required: false, scale: "number", range: 7 },
    ],
  },
  {
    id: "b3",
    name: "Block 3",
    elements: [
      {
        id: "q_pic",
        type: "pictureSelection",
        headline: { default: "Pick a picture" },
        required: false,
        choices: [
          { id: "p1", imageUrl: "https://example.com/1.png" },
          { id: "p2", imageUrl: "https://example.com/2.png" },
          { id: "3", imageUrl: "https://example.com/3.png" },
        ],
      },
      {
        id: "q_rank",
        type: "ranking",
        headline: { default: "Rank" },
        required: false,
        choices: [choice("k1", "Item 1", "Element 1"), choice("k2", "Item 2", "Element 2"), choice("k3", "Item 3", "Element 3")],
      },
      {
        id: "q_matrix",
        type: "matrix",
        headline: { default: "Matrix" },
        required: false,
        rows: [choice("r1", "Speed", "Tempo"), choice("r2", "Price", "Preis"), choice("r3", "Support")],
        columns: [choice("x1", "Good", "Gut"), choice("x2", "Bad", "Schlecht"), choice("x3", "OK")],
      },
    ],
  },
  {
    id: "b4",
    name: "Block 4",
    elements: [
      { id: "q_cta", type: "cta", headline: { default: "Visit" }, required: false, buttonExternal: true, buttonUrl: "https://example.com", ctaButtonLabel: { default: "Go" } },
      { id: "q_cta_internal", type: "cta", headline: { default: "Next" }, required: false, buttonExternal: false },
      { id: "q_consent", type: "consent", headline: { default: "Agree" }, required: false, label: { default: "I agree" } },
      { id: "q_cal", type: "cal", headline: { default: "Book" }, required: false, calUserName: "rick" },
      { id: "q_date", type: "date", headline: { default: "When" }, required: false },
      { id: "q_file", type: "fileUpload", headline: { default: "Upload" }, required: false },
      { id: "q_addr", type: "address", headline: { default: "Address" }, required: false },
      { id: "q_ci", type: "contactInfo", headline: { default: "Contact" }, required: false },
    ],
  },
];

const embeddedFields = [
  { storageKey: "utm", name: "utm", source: "ingested", dataType: "string" },
  { storageKey: "score", name: "score", source: "computed", dataType: "number" },
  { storageKey: "plan", name: "plan", source: "ingested", dataType: "string" },
];

const languages = [
  { language: { code: "en" }, default: true },
  { language: { code: "de" }, default: false },
];

const answer = (type, el, lang) => {
  const label = (c) => (lang === "de" && c.label.de ? c.label.de : c.label.default);
  switch (type) {
    case "openText":
      return pick(["Great", "", "Meh", "Needs work", "Great"]);
    case "multipleChoiceSingle":
      return pick([...el.choices.map(label), "Purple", "", "Rot"]);
    case "multipleChoiceMulti": {
      const out = el.choices.filter(() => chance(0.4)).map(label);
      if (chance(0.2)) out.push("Banana");
      if (chance(0.1)) out.push("");
      if (chance(0.05)) out.push(7);
      return out;
    }
    case "nps":
      return pick([0, 3, 6, 7, 8, 9, 10, 10, 9, 11, 5]);
    case "rating":
    case "csat":
      return pick([1, 2, 3, 4, 5, 5, 4, 7, 2.5, 0]);
    case "ces":
      return pick([1, 2, 3, 4, 5, 6, 7, 8]);
    case "pictureSelection":
      return el.choices.filter(() => chance(0.5)).map((c) => c.id).concat(chance(0.1) ? ["zzz"] : []).concat(chance(0.1) ? [3] : []);
    case "ranking": {
      const items = el.choices.map(label);
      if (chance(0.1)) items.push("Unknown");
      return items.sort(() => rand() - 0.5);
    }
    case "matrix": {
      const out = {};
      for (const row of el.rows) {
        if (chance(0.8)) out[label(row)] = pick([...el.columns.map(label), "Nope"]);
      }
      return out;
    }
    case "cta":
      return pick(["clicked", "dismissed", "clicked", ""]);
    case "consent":
      return pick(["accepted", "accepted", "", "dismissed"]);
    case "cal":
      return pick(["booked", "", "booked"]);
    case "date":
      return pick(["2026-01-01", "2026-02-03", ""]);
    case "fileUpload":
      return pick([["https://example.com/a.pdf"], [], ["/storage/w/public/x.png", "https://example.com/b.pdf"]]);
    case "address":
    case "contactInfo":
      return pick([["Ada", "Lovelace", "", "", ""], [], ["Grace", "Hopper", "grace@example.com", "", ""]]);
  }
};

const elements = blocks.flatMap((b) => b.elements);

const makeResponses = (n) => {
  const responses = [];
  for (let i = 0; i < n; i++) {
    const language = pick([null, "en", "de", "de", "fr", "default"]);
    const lang = language === "de" ? "de" : "default";
    const data = {};
    const ttc = {};
    const finished = chance(0.6);
    // A response reaches a random depth; partial ones stop early.
    const depth = finished ? elements.length : int(0, elements.length);
    elements.slice(0, depth).forEach((el) => {
      const r = rand();
      if (r < 0.7) {
        data[el.id] = answer(el.type, el, lang);
        if (chance(0.9)) ttc[el.id] = int(100, 9000);
      } else if (r < 0.85) {
        ttc[el.id] = int(100, 4000);
      }
    });
    if (chance(0.5)) data.utm = pick(["google", "newsletter", "", "twitter"]);
    if (chance(0.3)) data.plan = pick(["pro", 42, "free"]);
    if (finished) ttc._total = Object.values(ttc).reduce((a, b) => a + b, 0);
    if (chance(0.05)) {
      for (const k of Object.keys(ttc)) delete ttc[k];
      if (finished) ttc._total = int(1000, 5000);
    }
    const hasContact = chance(0.3);
    responses.push({
      id: uuid(i + 1),
      data,
      updatedAt: new Date(Date.UTC(2026, 0, 1, 0, 0, n - i)).toISOString().replace(".000Z", "Z"),
      contact: hasContact ? (chance(0.5) ? { id: uuid(1000 + i), userId: `user-${i}` } : { id: uuid(1000 + i) }) : null,
      contactAttributes: hasContact ? { email: `u${i}@example.com`, userId: `user-${i}` } : {},
      language,
      ttc,
      finished,
    });
  }
  return responses;
};

const scenario = (name, welcomeCardEnabled, n, displayCount, quotas) => {
  const survey = { id: "survey", blocks, languages, welcomeCard: { enabled: welcomeCardEnabled }, embeddedFields };
  const responses = makeResponses(n);
  const meta = getSurveySummaryMeta(survey, responses, displayCount, quotas);
  const dropOff = getSurveySummaryDropOff(survey, elements, responses, displayCount);
  return getElementSummary(survey, elements, responses, dropOff).then((summary) => {
    writeFileSync(`out/${name}.json`, JSON.stringify({ survey, responses, displayCount, quotas, expected: { meta, dropOff, summary, quotas } }));
  });
};

const quotas = [
  { id: uuid(9001), name: "Full", limit: 10, count: 10, percentage: 100 },
  { id: uuid(9002), name: "Half", limit: 20, count: 10, percentage: 50 },
  { id: uuid(9003), name: "Over", limit: 3, count: 5, percentage: 166.67 },
];

await scenario("welcome-disabled", false, 120, 160, quotas);
await scenario("welcome-enabled", true, 60, 40, []);
await scenario("no-responses", false, 0, 12, []);
