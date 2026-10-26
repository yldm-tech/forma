// The embed script customer pages load from /js/embed.js: it renders every element marked with one of the MODE_ATTRIBUTES as a survey frame that follows its survey's height, or as a popup, slider or side tab, and exposes window.formaEmbed for pages that open a survey from their own code or add embed elements later.
import { type FormaEmbed, createFormaEmbed } from "./embed";

declare global {
  interface Window {
    formaEmbed?: FormaEmbed;
  }
}

// A second copy of the script on the page (two snippets pasted) renders nothing twice: it reuses the first one's API and only looks for new elements.
const embed = (window.formaEmbed ??= createFormaEmbed(window));

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", () => embed.scan(), { once: true });
} else {
  embed.scan();
}
