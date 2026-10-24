import type { TContactAttributeKey } from "@forma/types/contact-attribute-key";
import type { TBaseFilters, TSegment } from "@forma/types/segment";
import type { SetStateAction } from "react";
import type { AttributeKey } from "../../../lib/attribute-keys";
import type { Segment } from "../../../lib/queries/contacts";

// The segment editor (components/segment-editor.tsx) edits a TSegment, as the survey editor's targeting card holds one. The segments page holds a form instead: a title, a description and a filter tree. These turn the page's data into what the editor reads and the editor's changes back into the form's tree.

/** An attribute key as the server lists it, with the dates the editor's type carries. */
export const toContactAttributeKey = (key: AttributeKey): TContactAttributeKey => ({
  ...key,
  createdAt: new Date(key.createdAt),
  updatedAt: new Date(key.updatedAt),
});

/** A listed segment as the editor's segment filters and add-filter modal read it. */
export const toEditorSegment = (segment: Segment): TSegment => ({
  id: segment.id,
  title: segment.title,
  description: segment.description,
  isPrivate: segment.isPrivate,
  filters: segment.filters,
  workspaceId: segment.workspaceId,
  createdAt: new Date(segment.createdAt),
  updatedAt: new Date(segment.updatedAt),
  surveys: segment.surveys,
});

/**
 * The segment the editor edits on a segment form: its filters are the form's tree, its surveys those the segment gates (a survey-interaction filter may not name them), and the rest is not read by the editor. A new segment has no id yet.
 */
export const formSegment = ({
  segmentId,
  workspaceId,
  filters,
  linkedSurveyIds,
}: {
  segmentId?: string;
  workspaceId: string;
  filters: TBaseFilters;
  linkedSurveyIds: string[];
}): TSegment => ({
  id: segmentId ?? "",
  title: "",
  description: null,
  isPrivate: false,
  filters,
  workspaceId,
  createdAt: new Date(0),
  updatedAt: new Date(0),
  surveys: linkedSurveyIds,
});

/** The editor's setSegment on a form: the tree of the segment it sets becomes the form's tree. The editor never clears the segment; a null is ignored. */
export const formSetSegment =
  (current: TSegment, setFilters: (filters: TBaseFilters) => void) =>
  (next: SetStateAction<TSegment | null>): void => {
    const segment = typeof next === "function" ? next(current) : next;
    if (segment) setFilters(segment.filters);
  };
