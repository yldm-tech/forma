// @vitest-environment happy-dom
import { afterEach, describe, expect, test } from "vite-plus/test";
import { canMoveFocusInto } from "./focus-guard";

const mount = (html: string) => {
  document.body.innerHTML = html;
};

afterEach(() => {
  document.body.innerHTML = "";
});

describe("canMoveFocusInto", () => {
  test("nothing focused: the survey may take focus", () => {
    mount('<div id="fbjs"><div id="card"><button id="next">Next</button></div></div>');
    expect(canMoveFocusInto(document.getElementById("card") as HTMLElement)).toBe(true);
  });

  test("focus inside the same survey: the survey may move it", () => {
    mount('<div id="fbjs"><button id="back">Back</button><div id="card"><input id="answer" /></div></div>');
    (document.getElementById("back") as HTMLElement).focus();
    expect(canMoveFocusInto(document.getElementById("card") as HTMLElement)).toBe(true);
  });

  test("focus in the host page: the survey leaves it alone", () => {
    mount('<input id="editor-field" /><div id="fbjs"><div id="card"><input id="answer" /></div></div>');
    (document.getElementById("editor-field") as HTMLElement).focus();
    expect(canMoveFocusInto(document.getElementById("card") as HTMLElement)).toBe(false);
  });

  test("focus in another survey on the same page: left alone", () => {
    mount(
      '<div id="fbjs"><input id="other" /></div><div class="second"><div id="card"><input id="answer" /></div></div>'
    );
    (document.getElementById("other") as HTMLElement).focus();
    expect(canMoveFocusInto(document.getElementById("card") as HTMLElement)).toBe(false);
  });
});
