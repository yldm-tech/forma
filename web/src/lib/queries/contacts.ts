import type { TContactAttributeDataType } from "@forma/types/contact-attribute-key";
import type { TResponseWithQuotas } from "@forma/types/responses";
import type { TBaseFilters } from "@forma/types/segment";
import { infiniteQueryOptions, queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { parseResponse } from "../../features/survey-analysis/api";
import { api } from "../api";
import type { AttributeKey } from "../attribute-keys";
import type { DuplicateContactsAction } from "../contacts-csv";

/** forma2's ITEMS_PER_PAGE, the contacts list's page size. */
export const CONTACTS_PER_PAGE = 30;

/** A contact with its attribute values by key. */
export interface Contact {
  id: string;
  createdAt: string;
  updatedAt: string;
  workspaceId: string;
  attributes: Record<string, string>;
}

/** One of a contact's values with its key. */
export interface ContactAttribute {
  key: string;
  name: string | null;
  type: "default" | "custom";
  value: string;
  dataType: TContactAttributeDataType;
}

export interface ContactDetail {
  contact: Contact;
  attributes: ContactAttribute[];
}

/** A message the server sends back from an attribute edit, translated by its code. */
export interface AttributeUpdateMessage {
  code: string;
  params: Record<string, string>;
}

export interface AttributeUpdateResult {
  updatedAttributes: Record<string, string>;
  messages?: AttributeUpdateMessage[];
  errors?: AttributeUpdateMessage[];
  updatedAttributeKeys?: AttributeKey[];
}

export interface SurveyRef {
  id: string;
  name: string;
}

export interface Segment {
  id: string;
  createdAt: string;
  updatedAt: string;
  title: string;
  description: string | null;
  isPrivate: boolean;
  filters: TBaseFilters;
  workspaceId: string;
  surveys: string[];
  activeSurveys: SurveyRef[];
  inactiveSurveys: SurveyRef[];
}

/** A survey offered by the survey-interaction filter. */
export interface SegmentSurvey {
  id: string;
  name: string;
  status: string;
}

export interface SegmentUpdate {
  title?: string;
  description?: string | null;
  isPrivate?: boolean;
  filters?: TBaseFilters;
  surveys?: string[];
}

/** A display of a contact, forma2's Pick<TDisplay, "id" | "createdAt" | "surveyId">. */
export interface ContactDisplay {
  id: string;
  createdAt: Date;
  surveyId: string;
}

/** A contact's responses and displays, newest first, the surveys they belong to, and how many there are of each. */
export interface ContactActivity {
  responses: TResponseWithQuotas[];
  displays: ContactDisplay[];
  surveys: SurveyRef[];
  responseCount: number;
  displayCount: number;
}

type ContactActivityWire = Omit<ContactActivity, "responses" | "displays"> & {
  responses: Parameters<typeof parseResponse>[0][];
  displays: (Omit<ContactDisplay, "createdAt"> & { createdAt: string })[];
};

/** A CSV of personal links, as the share modal downloads it. */
export interface PersonalLinksFile {
  fileName: string;
  csvContent: string;
}

export const contactKeys = {
  attributeKeys: (workspaceId: string) => ["contact-attribute-keys", workspaceId] as const,
  attributeValues: (keyId: string) => ["contact-attribute-keys", "values", keyId] as const,
  all: (workspaceId: string) => ["contacts", workspaceId] as const,
  lists: (workspaceId: string) => ["contacts", workspaceId, "list"] as const,
  search: (workspaceId: string, search: string) => ["contacts", workspaceId, "list", search] as const,
  detail: (workspaceId: string, contactId: string) => ["contacts", workspaceId, "detail", contactId] as const,
  activity: (workspaceId: string, contactId: string) =>
    ["contacts", workspaceId, "detail", contactId, "activity"] as const,
  publishedLinkSurveys: (workspaceId: string) => ["contacts", workspaceId, "published-link-surveys"] as const,
  segments: (workspaceId: string) => ["segments", workspaceId] as const,
  segmentSurveys: (workspaceId: string) => ["segments", workspaceId, "surveys"] as const,
};

export const attributeKeysQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: contactKeys.attributeKeys(workspaceId),
    queryFn: ({ signal }) =>
      api.get<AttributeKey[]>(`/api/app/workspaces/${workspaceId}/contact-attribute-keys`, { signal }),
  });

/** Up to 100 distinct values of a key, offered by the segment editor. */
export const attributeValuesQuery = (keyId: string) =>
  queryOptions({
    queryKey: contactKeys.attributeValues(keyId),
    queryFn: ({ signal }) => api.get<string[]>(`/api/app/contact-attribute-keys/${keyId}/values`, { signal }),
  });

/** forma2's contacts list: pages of 30, newest first, searched by attribute value or exact id; the next page starts after the contacts loaded so far. */
export const contactsQuery = (workspaceId: string, search: string) =>
  infiniteQueryOptions({
    queryKey: contactKeys.search(workspaceId, search),
    initialPageParam: 0,
    queryFn: ({ pageParam, signal }) => {
      const params = new URLSearchParams({ offset: String(pageParam) });
      if (search) params.set("search", search);
      return api.get<Contact[]>(`/api/app/workspaces/${workspaceId}/contacts?${params.toString()}`, {
        signal,
      });
    },
    getNextPageParam: (lastPage, pages) =>
      lastPage.length >= CONTACTS_PER_PAGE
        ? pages.reduce((count, page) => count + page.length, 0)
        : undefined,
  });

export const contactQuery = (workspaceId: string, contactId: string) =>
  queryOptions({
    queryKey: contactKeys.detail(workspaceId, contactId),
    queryFn: ({ signal }) =>
      api.get<ContactDetail>(`/api/app/workspaces/${workspaceId}/contacts/${contactId}`, { signal }),
  });

/** forma2's contact activity: what getResponsesByContactId and getDisplaysByContactId loaded for the contact page. */
export const contactActivityQuery = (workspaceId: string, contactId: string) =>
  queryOptions({
    queryKey: contactKeys.activity(workspaceId, contactId),
    queryFn: ({ signal }) =>
      api
        .get<ContactActivityWire>(`/api/app/contacts/${contactId}/activity`, { signal })
        .then((raw): ContactActivity => ({
          ...raw,
          responses: raw.responses.map(parseResponse),
          displays: raw.displays.map((display) => ({ ...display, createdAt: new Date(display.createdAt) })),
        })),
  });

/** forma2's getPublishedLinkSurveys: the link surveys in progress a contact can get a personal link to. */
export const publishedLinkSurveysQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: contactKeys.publishedLinkSurveys(workspaceId),
    queryFn: ({ signal }) =>
      api.get<SurveyRef[]>(`/api/app/workspaces/${workspaceId}/published-link-surveys`, { signal }),
  });

export const segmentsQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: contactKeys.segments(workspaceId),
    queryFn: ({ signal }) => api.get<Segment[]>(`/api/app/workspaces/${workspaceId}/segments`, { signal }),
  });

export const segmentSurveysQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: contactKeys.segmentSurveys(workspaceId),
    queryFn: ({ signal }) =>
      api.get<SegmentSurvey[]>(`/api/app/workspaces/${workspaceId}/segment-surveys`, { signal }),
  });

export const deleteContact = (contactId: string) => api.delete<Contact>(`/api/app/contacts/${contactId}`);

/** Deleting contacts refreshes every list of the workspace's contacts. */
export const useDeleteContacts = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    // forma2 deleted a selection five at a time.
    mutationFn: async (contactIds: string[]) => {
      for (let i = 0; i < contactIds.length; i += 5) {
        await Promise.all(contactIds.slice(i, i + 5).map(deleteContact));
      }
    },
    // Only the lists: a deleted contact's own page is being left, and refetching it would fail.
    onSettled: () => queryClient.invalidateQueries({ queryKey: contactKeys.lists(workspaceId) }),
  });
};

export const useUpdateContactAttributes = (workspaceId: string, contactId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (attributes: Record<string, string | number>) =>
      api.put<AttributeUpdateResult>(`/api/app/contacts/${contactId}/attributes`, { attributes }),
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: contactKeys.all(workspaceId) }),
        queryClient.invalidateQueries({ queryKey: contactKeys.attributeKeys(workspaceId) }),
      ]),
  });
};

export const useImportContacts = (workspaceId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      file,
      duplicateContactsAction,
      attributeMap,
    }: {
      file: File;
      duplicateContactsAction: DuplicateContactsAction;
      attributeMap: Record<string, string>;
    }) => {
      const form = new FormData();
      form.append("file", file);
      form.append("duplicateContactsAction", duplicateContactsAction);
      form.append("attributeMap", JSON.stringify(attributeMap));
      return api.post<{ contacts: Contact[] }>(`/api/app/workspaces/${workspaceId}/contacts/import`, form);
    },
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: contactKeys.lists(workspaceId) }),
        queryClient.invalidateQueries({ queryKey: contactKeys.attributeKeys(workspaceId) }),
      ]),
  });
};

const useKeyMutation = <V>(workspaceId: string, mutationFn: (variables: V) => Promise<unknown>) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: contactKeys.attributeKeys(workspaceId) }),
  });
};

export const useCreateAttributeKey = (workspaceId: string) =>
  useKeyMutation(
    workspaceId,
    (input: { key: string; name?: string; description?: string; dataType?: TContactAttributeDataType }) =>
      api.post<AttributeKey>(`/api/app/workspaces/${workspaceId}/contact-attribute-keys`, input)
  );

export const useUpdateAttributeKey = (workspaceId: string) =>
  useKeyMutation(
    workspaceId,
    ({ id, name, description }: { id: string; name?: string; description?: string }) =>
      api.patch<AttributeKey>(`/api/app/contact-attribute-keys/${id}`, { name, description })
  );

export const deleteAttributeKey = (keyId: string) =>
  api.delete<AttributeKey>(`/api/app/contact-attribute-keys/${keyId}`);

export const useDeleteAttributeKeys = (workspaceId: string) =>
  useKeyMutation(workspaceId, async (keyIds: string[]) => {
    for (let i = 0; i < keyIds.length; i += 5) {
      await Promise.all(keyIds.slice(i, i + 5).map(deleteAttributeKey));
    }
  });

const useSegmentMutation = <V, R>(workspaceId: string, mutationFn: (variables: V) => Promise<R>) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: contactKeys.segments(workspaceId) }),
  });
};

export const useCreateSegment = (workspaceId: string) =>
  useSegmentMutation(
    workspaceId,
    (input: {
      title: string;
      description?: string;
      isPrivate: boolean;
      filters: TBaseFilters;
      surveyId: string;
    }) => api.post<Segment>(`/api/app/workspaces/${workspaceId}/segments`, input)
  );

export const useUpdateSegment = (workspaceId: string) =>
  useSegmentMutation(workspaceId, ({ id, update }: { id: string; update: SegmentUpdate }) =>
    api.patch<Segment>(`/api/app/segments/${id}`, update)
  );

export const useDeleteSegment = (workspaceId: string) =>
  useSegmentMutation(workspaceId, (id: string) => api.delete<Segment>(`/api/app/segments/${id}`));

/** forma2's generatePersonalSurveyLinkAction: the contact's personal link to a survey, expiring after expirationDays when given. */
export const generatePersonalLink = (contactId: string, surveyId: string, expirationDays?: number) =>
  api.post<{ surveyUrl: string }>(`/api/app/contacts/${contactId}/personal-link`, {
    surveyId,
    expirationDays,
  });

/** forma2's generatePersonalLinksAction: a CSV of personal links for the contacts of a segment. */
export const generatePersonalLinks = (surveyId: string, segmentId: string, expirationDays?: number) =>
  api.post<PersonalLinksFile>(`/api/app/surveys/${surveyId}/personal-links`, { segmentId, expirationDays });
