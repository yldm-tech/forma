import { describe, expect, test } from "vite-plus/test";
import { isArrowKeyOwner } from "./response-card-modal";

const element = (html: string, selector: string) => {
  const host = document.createElement("div");
  host.innerHTML = html;
  document.body.append(host);
  return host.querySelector(selector);
};

describe("isArrowKeyOwner", () => {
  test("leaves the arrows to fields, editable regions and composite widgets", () => {
    expect(isArrowKeyOwner(element("<input id='a' />", "#a"))).toBe(true);
    expect(isArrowKeyOwner(element("<textarea id='b'></textarea>", "#b"))).toBe(true);
    expect(isArrowKeyOwner(element("<div contenteditable='true'><p id='c'>x</p></div>", "#c"))).toBe(true);
    expect(isArrowKeyOwner(element("<div role='combobox'><span id='d'>x</span></div>", "#d"))).toBe(true);
    expect(isArrowKeyOwner(element("<div role='menu'><div role='menuitem' id='e'>x</div></div>", "#e"))).toBe(
      true
    );
  });

  test("takes the arrows on buttons and plain content, so the card steps", () => {
    expect(isArrowKeyOwner(element("<button id='f'>Next</button>", "#f"))).toBe(false);
    expect(isArrowKeyOwner(element("<p id='g'>Answer</p>", "#g"))).toBe(false);
    expect(isArrowKeyOwner(null)).toBe(false);
  });
});
