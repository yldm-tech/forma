// The only script every page loads: the header's toggles, the locale hint, reveal-on-scroll, the copy buttons, and the decision whether to load the hero scene. Everything it touches is already in the HTML and readable without it.
import "../styles.css";

const LOCALE_HINT_KEY = "forma-site-locale-hint";

document.documentElement.classList.add("js");

// ---- disclosures: the Product menu and the language menu ----

const disclosures = [...document.querySelectorAll<HTMLButtonElement>("[data-disclosure]")];

const panelOf = (button: HTMLButtonElement): HTMLElement | null =>
  document.getElementById(button.getAttribute("aria-controls") ?? "");

const setOpen = (button: HTMLButtonElement, open: boolean): void => {
  button.setAttribute("aria-expanded", String(open));
  const panel = panelOf(button);
  if (panel) panel.hidden = !open;
};

const closeAll = (except?: HTMLButtonElement): void => {
  for (const button of disclosures) if (button !== except) setOpen(button, false);
};

for (const button of disclosures) {
  button.addEventListener("click", () => {
    const open = button.getAttribute("aria-expanded") !== "true";
    closeAll(button);
    setOpen(button, open);
  });
  const root = button.closest("[data-disclosure-root]");
  root?.addEventListener("keydown", (event) => {
    if ((event as KeyboardEvent).key === "Escape" && button.getAttribute("aria-expanded") === "true") {
      setOpen(button, false);
      button.focus();
    }
  });
  root?.addEventListener("focusout", (event) => {
    const next = (event as FocusEvent).relatedTarget;
    if (next instanceof Node && !root.contains(next)) setOpen(button, false);
  });
}

document.addEventListener("click", (event) => {
  const target = event.target;
  if (target instanceof Element && !target.closest("[data-disclosure-root]")) closeAll();
});

// ---- mobile sheet: a modal dialog with its own focus loop ----

const sheet = document.getElementById("mobile-sheet");
const sheetOpener = document.querySelector<HTMLButtonElement>("[data-sheet-open]");

const focusables = (container: HTMLElement): HTMLElement[] =>
  [...container.querySelectorAll<HTMLElement>("a[href], button:not([disabled])")].filter(
    (element) => element.offsetParent !== null
  );

const closeSheet = (): void => {
  if (!sheet || sheet.hidden) return;
  sheet.hidden = true;
  document.documentElement.style.overflow = "";
  sheetOpener?.setAttribute("aria-expanded", "false");
  sheetOpener?.focus();
};

if (sheet && sheetOpener) {
  sheetOpener.addEventListener("click", () => {
    sheet.hidden = false;
    document.documentElement.style.overflow = "hidden";
    sheetOpener.setAttribute("aria-expanded", "true");
    sheet.querySelector<HTMLButtonElement>("[data-sheet-close]")?.focus();
  });
  sheet.querySelector("[data-sheet-close]")?.addEventListener("click", closeSheet);
  sheet.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      closeSheet();
      return;
    }
    if (event.key !== "Tab") return;
    const items = focusables(sheet);
    const first = items[0];
    const last = items.at(-1);
    if (!first || !last) return;
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });
  // A wide screen has no sheet; close it if the window grows past the breakpoint while it is open.
  matchMedia("(min-width: 64rem)").addEventListener("change", (query) => {
    if (query.matches) closeSheet();
  });
}

// ---- header border once the page has scrolled ----

const header = document.querySelector<HTMLElement>("[data-site-header]");
if (header) {
  const update = (): void => {
    header.toggleAttribute("data-scrolled", window.scrollY > 8);
  };
  update();
  window.addEventListener("scroll", update, { passive: true });
}

// ---- locale hint: offered, never redirected (caching and search engines need one URL per language) ----

const showLocaleHint = (): void => {
  const slot = document.querySelector("[data-locale-hint-slot]");
  const language = navigator.language.toLowerCase();
  const wanted = language.startsWith("zh") ? "zh" : language.startsWith("ja") ? "ja" : undefined;
  if (!slot || !wanted) return;
  const template = document.querySelector<HTMLTemplateElement>(`template[data-locale-hint="${wanted}"]`);
  if (!template) return;
  try {
    if (localStorage.getItem(LOCALE_HINT_KEY) === "dismissed") return;
  } catch {
    // Storage blocked: show the hint; dismissing it then lasts for this page only.
  }
  const bar = template.content.firstElementChild?.cloneNode(true);
  if (!(bar instanceof HTMLElement)) return;
  bar.querySelector("[data-locale-hint-dismiss]")?.addEventListener("click", () => {
    bar.remove();
    try {
      localStorage.setItem(LOCALE_HINT_KEY, "dismissed");
    } catch {
      // Storage blocked; nothing to remember it in.
    }
  });
  slot.append(bar);
};
showLocaleHint();

// ---- reveal on scroll: only what is still below the fold, and never with reduced motion ----

const reducedMotion = matchMedia("(prefers-reduced-motion: reduce)");
if (!reducedMotion.matches && "IntersectionObserver" in window) {
  const pending = [...document.querySelectorAll<HTMLElement>("[data-reveal]")].filter(
    (element) => element.getBoundingClientRect().top > window.innerHeight
  );
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        entry.target.classList.add("reveal-in");
        entry.target.classList.remove("reveal-pending");
        observer.unobserve(entry.target);
      }
    },
    { rootMargin: "0px 0px -10% 0px" }
  );
  for (const element of pending) {
    element.classList.add("reveal-pending");
    observer.observe(element);
  }
}

// ---- copy buttons on code blocks ----

for (const block of document.querySelectorAll<HTMLElement>("[data-code-block]")) {
  const button = block.querySelector<HTMLButtonElement>("[data-copy]");
  const status = block.querySelector<HTMLElement>("[data-copy-status]");
  const code = block.querySelector("code")?.textContent;
  if (!button || !status || code === undefined || code === null || !navigator.clipboard) continue;
  button.hidden = false;
  button.addEventListener("click", () => {
    void navigator.clipboard.writeText(code).then(() => {
      status.textContent = button.dataset.copiedLabel ?? "";
      window.setTimeout(() => (status.textContent = ""), 2000);
    });
  });
}

// ---- hero scene: loaded after the page, only where it can run well ----

const canRunHeroScene = (): boolean => {
  if (reducedMotion.matches) return false;
  if (window.innerWidth < 768) return false;
  const nav = navigator as Navigator & { connection?: { saveData?: boolean }; deviceMemory?: number };
  if (nav.connection?.saveData) return false;
  if (nav.deviceMemory !== undefined && nav.deviceMemory <= 2) return false;
  // three.js r163+ renders with WebGL 2 only, so a WebGL 1-only device keeps the static picture.
  const probe = document.createElement("canvas").getContext("webgl2");
  probe?.getExtension("WEBGL_lose_context")?.loseContext();
  return probe !== null;
};

const loadHeroScene = (): void => {
  const container = document.querySelector<HTMLElement>("[data-hero]");
  if (!container || !canRunHeroScene()) return;
  import("./hero-scene")
    .then(({ mountHeroScene }) => mountHeroScene(container, reducedMotion))
    .catch(() => {
      // The static SVG is already showing; a failed load changes nothing.
    });
};

const whenIdle = (callback: () => void): void => {
  if ("requestIdleCallback" in window) window.requestIdleCallback(callback, { timeout: 2000 });
  else setTimeout(callback, 200);
};

if (document.readyState === "complete") whenIdle(loadHeroScene);
else window.addEventListener("load", () => whenIdle(loadHeroScene), { once: true });
