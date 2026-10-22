// The Go server sends timestamps as ISO strings; the pages ported from forma2 work on its types, whose timestamps are Dates. These helpers turn one into the other at the API boundary.

/** A record as the server sends it: its timestamps are ISO strings. */
export type Stamped<T extends { createdAt: Date; updatedAt: Date }> = Omit<T, "createdAt" | "updatedAt"> & {
  createdAt: string;
  updatedAt: string;
};

/** The record with its createdAt and updatedAt as Dates. */
export const stamped = <T extends { createdAt: Date; updatedAt: Date }>(value: Stamped<T>): T =>
  ({ ...value, createdAt: new Date(value.createdAt), updatedAt: new Date(value.updatedAt) }) as T;

/** An optional ISO timestamp as a Date, keeping null and undefined as they are. */
export const optionalDate = (value: string | null | undefined): Date | null | undefined =>
  value === null || value === undefined ? value : new Date(value);
