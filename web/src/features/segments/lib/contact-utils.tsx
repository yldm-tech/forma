import { Calendar1Icon, HashIcon, TagIcon } from "lucide-react";
import { TContactAttributeDataType } from "@forma/types/contact-attribute-key";

export const getContactAttributeDataTypeIcon = (dataType: TContactAttributeDataType) => {
  switch (dataType) {
    case "date":
      return <Calendar1Icon className="size-4" />;
    case "number":
      return <HashIcon className="size-4" />;
    case "string":
    default:
      return <TagIcon className="size-4" />;
  }
};
