import "server-only";
import { type TV3WorkspaceListParams, listV3WorkspaceResource } from "@/app/api/v3/lib/list-resource";
import { getContactAttributeKeys } from "@/modules/contacts/lib/contact-attribute-keys";
import { serializeV3ContactAttributeKey } from "../serializers";

export function listV3ContactAttributeKeys(params: TV3WorkspaceListParams): Promise<Response> {
  return listV3WorkspaceResource({
    ...params,
    resourceName: "contact attribute keys",
    fetchAll: getContactAttributeKeys,
    serialize: serializeV3ContactAttributeKey,
  });
}
