import type { TBaseFilters } from "@forma/types/segment";
import { doesSegmentReferenceSegment, isResourceFilter } from "../../lib/segment-filters";

/** Whether a tree has an attribute filter on one of the keys, at any depth. */
const usesAttributeKey = (filters: TBaseFilters, keys: Set<string>): boolean =>
  filters.some(({ resource }) =>
    isResourceFilter(resource)
      ? resource.root.type === "attribute" && keys.has(resource.root.contactAttributeKey)
      : usesAttributeKey(resource, keys)
  );

interface SegmentUsage {
  id: string;
  title: string;
  isPrivate: boolean;
  filters: TBaseFilters;
  activeSurveys: { id: string; name: string }[];
  inactiveSurveys: { id: string; name: string }[];
}

/**
 * What deleting attribute keys would affect: the segments whose filters use one of them, directly or through a "user is (not) in" filter on such a segment, and the surveys those segments target. Public segments are listed by title; a survey's private segment is only listed through its survey. Each name appears once.
 */
export const attributeKeyUsage = (
  segments: SegmentUsage[],
  keys: string[]
): { segments: string[]; surveys: string[] } => {
  const keySet = new Set(keys);
  const affected = new Set(
    segments.filter((segment) => usesAttributeKey(segment.filters, keySet)).map((s) => s.id)
  );
  // A segment that filters on an affected segment matches differently too; repeat until no more are found.
  let grew = affected.size > 0;
  while (grew) {
    grew = false;
    for (const segment of segments) {
      if (affected.has(segment.id)) continue;
      if ([...affected].some((id) => doesSegmentReferenceSegment(segment.filters, id))) {
        affected.add(segment.id);
        grew = true;
      }
    }
  }
  const hit = segments.filter((segment) => affected.has(segment.id));
  const surveys = new Map<string, string>();
  for (const segment of hit) {
    for (const survey of [...segment.activeSurveys, ...segment.inactiveSurveys])
      surveys.set(survey.id, survey.name);
  }
  return {
    segments: hit.filter((segment) => !segment.isPrivate).map((segment) => segment.title),
    surveys: [...surveys.values()],
  };
};
