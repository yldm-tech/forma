/**
 * Which main-navigation sections survive the licence this installation actually holds.
 *
 * An entry for a feature the licence does not cover leads to a page whose only content is an
 * upsell, so the product reads as broken rather than as smaller. Hiding the entry is what keeps it
 * coherent — but a section whose every item is hidden would still render its heading, leaving a
 * label with nothing under it. "Act" holds only Workflows, so that is the ordinary case on an
 * unlicensed install rather than an edge one.
 */

export interface NavigationSectionLike<TItem extends { isHidden?: boolean }> {
  items: TItem[];
}

export const getVisibleNavigationSections = <
  TItem extends { isHidden?: boolean },
  TSection extends NavigationSectionLike<TItem>,
>(
  sections: TSection[]
): TSection[] => sections.filter((section) => section.items.some((item) => !item.isHidden));
