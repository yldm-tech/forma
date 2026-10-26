import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { useState } from "react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, describe, expect, test } from "vite-plus/test";
import { initI18n } from "../../../../lib/i18n";
import { type EmbedLayout, WebsiteEmbedTab, scriptEmbedCode, websiteEmbedCode } from "./website-embed-tab";

const SURVEY_URL = "https://s.example.com/s/0192f7a8-1c3e-7b2a-9d4f-5e6a7b8c9d12";
const SCRIPT_URL = "https://s.example.com/js/embed.js";
const BASE = { buttonText: "Take the survey", tabLabel: "Feedback", position: "right" } as const;

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
  // The layout switch measures its options to place its highlight; jsdom lays nothing out.
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

afterEach(() => {
  cleanup();
});

describe("scriptEmbedCode", () => {
  test("standard: an element the script fills with a frame that follows the survey", () => {
    expect(scriptEmbedCode(SURVEY_URL, SCRIPT_URL, { ...BASE, layout: "standard" })).toBe(
      `<div data-forma-embed="${SURVEY_URL}" style="width:100%"></div>\n<script src="${SCRIPT_URL}" async></script>`
    );
  });

  test("popup and slider: a button, or an element that opens on its own after the delay", () => {
    expect(scriptEmbedCode(SURVEY_URL, SCRIPT_URL, { ...BASE, layout: "popup" })).toBe(
      `<button type="button" data-forma-popup="${SURVEY_URL}">Take the survey</button>\n<script src="${SCRIPT_URL}" async></script>`
    );
    expect(scriptEmbedCode(SURVEY_URL, SCRIPT_URL, { ...BASE, layout: "popup", openDelay: 5 })).toBe(
      `<div data-forma-popup="${SURVEY_URL}" data-forma-open-delay="5"></div>\n<script src="${SCRIPT_URL}" async></script>`
    );
    expect(
      scriptEmbedCode(SURVEY_URL, SCRIPT_URL, { ...BASE, layout: "slider", position: "left" }).split("\n")[0]
    ).toBe(
      `<button type="button" data-forma-slider="${SURVEY_URL}" data-forma-position="left">Take the survey</button>`
    );
  });

  test("side tab: its label and edge", () => {
    expect(
      scriptEmbedCode(SURVEY_URL, SCRIPT_URL, { ...BASE, layout: "side-tab", position: "left" }).split(
        "\n"
      )[0]
    ).toBe(
      `<div data-forma-side-tab="${SURVEY_URL}" data-forma-label="Feedback" data-forma-position="left"></div>`
    );
  });

  test("escapes what the creator typed and what the link carries", () => {
    const code = scriptEmbedCode(`${SURVEY_URL}?lang=de&source="x"`, SCRIPT_URL, {
      ...BASE,
      layout: "popup",
      buttonText: `<img src=x onerror=alert(1)> & "go"`,
    });
    expect(code.split("\n")[0]).toBe(
      `<button type="button" data-forma-popup="${SURVEY_URL}?lang=de&amp;source=&quot;x&quot;">&lt;img src=x onerror=alert(1)&gt; &amp; &quot;go&quot;</button>`
    );
  });
});

describe("websiteEmbedCode", () => {
  test("the plain iframe keeps its fixed-height box, with embed mode as a choice", () => {
    expect(websiteEmbedCode(SURVEY_URL, true).iframeSrc).toBe(`${SURVEY_URL}?embed=true`);
    expect(websiteEmbedCode(`${SURVEY_URL}?lang=de`, false).iframeSrc).toBe(`${SURVEY_URL}?lang=de`);
    expect(websiteEmbedCode(SURVEY_URL, false).code).toContain("height:80dvh");
  });
});

const Tab = ({ initialLayout = "standard" }: { initialLayout?: EmbedLayout }) => {
  const [layout, setLayout] = useState<EmbedLayout>(initialLayout);
  return (
    <WebsiteEmbedTab
      surveyUrl={SURVEY_URL}
      publicDomain="https://s.example.com"
      layout={layout}
      onLayoutChange={setLayout}
    />
  );
};

const renderTab = (initialLayout?: EmbedLayout) =>
  render(
    <I18nextProvider i18n={i18n}>
      <Tab initialLayout={initialLayout} />
    </I18nextProvider>
  );

const code = () => document.querySelector("pre code")?.textContent ?? "";
const preview = () => screen.getByTitle("Preview") as HTMLIFrameElement;

describe("WebsiteEmbedTab", () => {
  test("starts on the standard layout, previewed on a sample page that loads the script", () => {
    renderTab();
    expect(code()).toContain(`data-forma-embed="${SURVEY_URL}"`);
    expect(code()).toContain(`<script src="${SCRIPT_URL}" async></script>`);
    const doc = preview().getAttribute("srcdoc") ?? "";
    expect(doc).toContain(`data-forma-embed="${SURVEY_URL}?preview=true"`);
    expect(doc).toContain(SCRIPT_URL);
  });

  test("switches layouts, with each layout's options", () => {
    renderTab();
    fireEvent.click(screen.getByRole("button", { name: "Popup" }));
    expect(code()).toContain(
      `<button type="button" data-forma-popup="${SURVEY_URL}">Take the survey</button>`
    );

    fireEvent.change(screen.getByLabelText("Button text"), { target: { value: "Tell us" } });
    expect(code()).toContain(">Tell us</button>");

    fireEvent.click(screen.getByRole("switch", { name: /Open after a delay/ }));
    expect(code()).toContain(`<div data-forma-popup="${SURVEY_URL}" data-forma-open-delay="5"></div>`);
    expect(screen.queryByLabelText("Button text")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Side tab" }));
    fireEvent.change(screen.getByLabelText("Tab label"), { target: { value: "Ideas?" } });
    fireEvent.click(screen.getByRole("button", { name: "Left" }));
    expect(code()).toContain(
      `<div data-forma-side-tab="${SURVEY_URL}" data-forma-label="Ideas?" data-forma-position="left"></div>`
    );
  });

  test("keeps the plain iframe, its embed mode and its direct preview", () => {
    renderTab("iframe");
    expect(code()).toContain(`src="${SURVEY_URL}"`);
    fireEvent.click(screen.getByRole("switch", { name: /Embed Mode/ }));
    expect(code()).toContain(`src="${SURVEY_URL}?embed=true"`);
    expect(preview().getAttribute("src")).toBe(`${SURVEY_URL}?embed=true&preview=true`);
  });
});
