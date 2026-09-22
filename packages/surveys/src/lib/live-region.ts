/**
 * The persistent, visually hidden status region that survey opens are announced into. The
 * @forma/js SDK mounts it at setup time (packages/js-core/src/lib/survey/widget.ts,
 * `addLiveRegionContainer`) so assistive tech has registered the region long before the first
 * message lands — screen readers only reliably announce changes made to a live region that
 * already existed, not a region inserted together with its content.
 *
 * Embed scripts stay on customer pages for years, so an older SDK may not create the region.
 * `ensureLiveRegion` re-creates it as a degraded fallback (the announcement is delayed a beat
 * to give assistive tech a chance to register the fresh region — see the caller).
 */

// Shipped id contract with the SDK (packages/js-core/src/lib/common/constants.ts) — must never change.
const LIVE_REGION_ID = "forma-live-region";

export const ensureLiveRegion = (): HTMLElement => {
  const existingRegion = document.getElementById(LIVE_REGION_ID);
  if (existingRegion) return existingRegion;

  const liveRegion = document.createElement("div");
  liveRegion.id = LIVE_REGION_ID;
  liveRegion.setAttribute("role", "status");
  liveRegion.setAttribute("aria-live", "polite");
  liveRegion.setAttribute("aria-atomic", "true");
  liveRegion.style.cssText =
    "position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0";
  document.body.appendChild(liveRegion);
  return liveRegion;
};

let pendingAnnouncement: ReturnType<typeof setTimeout> | undefined;

/**
 * Announce a message in the shared status region.
 *
 * The clear-then-set cannot be collapsed into a single assignment: assistive tech announces a live
 * region only when its text actually changes, so ranking an option back to the position it just left
 * would otherwise be silent. The message is written on a later task so the clear lands as a mutation
 * of its own, and a newer announcement supersedes one still waiting.
 */
export const announceToLiveRegion = (message: string): void => {
  const liveRegion = ensureLiveRegion();
  clearTimeout(pendingAnnouncement);
  liveRegion.textContent = "";
  pendingAnnouncement = setTimeout(() => {
    liveRegion.textContent = message;
  }, 0);
};
