"use client";

import { FileSpreadsheetIcon, FormIcon } from "lucide-react";
import { TFeedbackSourceType } from "@forma/types/feedback-source";

export const getFeedbackSourceIcon = (type: TFeedbackSourceType, className: string) => {
  switch (type) {
    case "forma_survey":
      return <FormIcon className={className} />;
    case "csv":
      return <FileSpreadsheetIcon className={className} />;
    /* exhausted */
  }
};

export const getFeedbackSourceTypeLabelKey = (type: TFeedbackSourceType): string => {
  switch (type) {
    case "forma_survey":
      return "workspace.unify.forma_surveys";
    case "csv":
      return "workspace.unify.csv_import";
    /* exhausted */
  }
};
