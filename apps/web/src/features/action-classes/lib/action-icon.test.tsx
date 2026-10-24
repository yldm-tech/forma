import { cleanup, render } from "@testing-library/react";
import { afterEach, expect, test } from "vite-plus/test";
import { ActionClassIcon, actionIconKey } from "./action-icon";

afterEach(cleanup);

test("an action's icon follows its type, and a no-code action's trigger kind", () => {
  expect(actionIconKey({ type: "code", noCodeConfig: null })).toBe("code");
  expect(actionIconKey({ type: "automatic", noCodeConfig: null })).toBe("automatic");
  expect(actionIconKey({ type: "noCode", noCodeConfig: { type: "click" } })).toBe("click");
  expect(actionIconKey({ type: "noCode", noCodeConfig: { type: "pageView" } })).toBe("pageView");
  expect(actionIconKey({ type: "noCode", noCodeConfig: { type: "exitIntent" } })).toBe("exitIntent");
  expect(actionIconKey({ type: "noCode", noCodeConfig: { type: "fiftyPercentScroll" } })).toBe(
    "fiftyPercentScroll"
  );
  expect(actionIconKey({ type: "noCode", noCodeConfig: { type: "pageDwell" } })).toBe("pageDwell");
});

test("a no-code action without a config, or with a kind this build does not know, shows the click icon", () => {
  expect(actionIconKey({ type: "noCode", noCodeConfig: null })).toBe("click");
  expect(actionIconKey({ type: "noCode" })).toBe("click");
  expect(actionIconKey({ type: "noCode", noCodeConfig: { type: "hover" } })).toBe("click");
  // A config key inherited from Object.prototype is not an icon.
  expect(actionIconKey({ type: "noCode", noCodeConfig: { type: "toString" } })).toBe("click");
});

test("the icon renders the lucide icon of its trigger kind, hidden from assistive technology", () => {
  const icon = (actionClass: { type: string; noCodeConfig?: { type: string } | null }) => {
    const { container, unmount } = render(<ActionClassIcon actionClass={actionClass} className="size-5" />);
    const svg = container.querySelector("svg");
    const result = { classes: svg?.getAttribute("class") ?? "", hidden: svg?.getAttribute("aria-hidden") };
    unmount();
    return result;
  };
  const cases: [{ type: string; noCodeConfig?: { type: string } | null }, string][] = [
    [{ type: "code" }, "lucide-code-xml"],
    [{ type: "automatic" }, "lucide-zap"],
    [{ type: "noCode", noCodeConfig: { type: "click" } }, "lucide-mouse-pointer-click"],
    [{ type: "noCode", noCodeConfig: { type: "pageView" } }, "lucide-eye"],
    [{ type: "noCode", noCodeConfig: { type: "exitIntent" } }, "lucide-log-out"],
    [{ type: "noCode", noCodeConfig: { type: "fiftyPercentScroll" } }, "lucide-arrow-down-to-line"],
    [{ type: "noCode", noCodeConfig: { type: "pageDwell" } }, "lucide-timer"],
  ];
  for (const [actionClass, lucideClass] of cases) {
    const { classes, hidden } = icon(actionClass);
    expect(classes.split(" ")).toContain(lucideClass);
    expect(classes.split(" ")).toContain("size-5");
    expect(hidden).toBe("true");
  }
});
