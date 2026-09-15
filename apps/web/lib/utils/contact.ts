import { TContactAttributes } from "@forma/types/contact-attribute";
import { TResponseContact } from "@forma/types/responses";

export const getContactIdentifier = (
  contact: TResponseContact | null,
  contactAttributes: TContactAttributes | null
): string => {
  return contactAttributes?.email || contact?.userId || "";
};
