// @vitest-environment happy-dom
import { afterEach, describe, expect, test, vi } from "vite-plus/test";
import { MIN_VISIBLE_ANSWERS_PX, scrollToFailedElement, shouldLeadWithHeadline } from "./error-scroll";

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

const rectAt = (top: number) => ({ top }) as DOMRect;

/** A question inside the card's scroller, its answers starting `headerHeight` below its top. */
const mountQuestion = (headerHeight: number, scrollerHeight: number) => {
  document.body.innerHTML = `
    <div data-survey-scroller style="scroll-padding-top: 16px; scroll-padding-bottom: 72px">
      <form id="question"><h3>What best describes your role?</h3><div data-element-input id="answers"></div></form>
    </div>`;
  const scroller = document.querySelector<HTMLElement>("[data-survey-scroller]") as HTMLElement;
  const form = document.getElementById("question") as HTMLElement;
  const answers = document.getElementById("answers") as HTMLElement;
  Object.defineProperty(scroller, "clientHeight", { value: scrollerHeight });
  vi.spyOn(form, "getBoundingClientRect").mockReturnValue(rectAt(100));
  vi.spyOn(answers, "getBoundingClientRect").mockReturnValue(rectAt(100 + headerHeight));
  const formScroll = vi.fn();
  const answersScroll = vi.fn();
  form.scrollIntoView = formScroll;
  answers.scrollIntoView = answersScroll;
  return { form, formScroll, answersScroll };
};

describe("shouldLeadWithHeadline", () => {
  test("the headline leads while the error and the first answer fit under it", () => {
    expect(shouldLeadWithHeadline(60, 60 + MIN_VISIBLE_ANSWERS_PX)).toBe(true);
    expect(shouldLeadWithHeadline(61, 60 + MIN_VISIBLE_ANSWERS_PX)).toBe(false);
  });
});

describe("scrollToFailedElement", () => {
  // A 360x640 phone: the card scrolls in about 400px, and a short headline used to end up above the card's top when the answers were centred.
  test("a short headline is scrolled to the top of the card, above its answers", () => {
    const { form, formScroll, answersScroll } = mountQuestion(56, 400);
    scrollToFailedElement(form, "auto");
    expect(formScroll).toHaveBeenCalledWith({ behavior: "auto", block: "start" });
    expect(answersScroll).not.toHaveBeenCalled();
  });

  test("a header too tall to leave room for the error brings the answers to the top instead", () => {
    // 400px less the 88px of scroll padding leaves 312px; a 260px header leaves less than one answer under it.
    const { form, formScroll, answersScroll } = mountQuestion(260, 400);
    scrollToFailedElement(form, "smooth");
    expect(answersScroll).toHaveBeenCalledWith({ behavior: "smooth", block: "start" });
    expect(formScroll).not.toHaveBeenCalled();
  });
});
