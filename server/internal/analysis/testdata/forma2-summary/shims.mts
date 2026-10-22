export const TSurveyElementTypeEnum = {
  FileUpload: "fileUpload", OpenText: "openText", MultipleChoiceSingle: "multipleChoiceSingle", MultipleChoiceMulti: "multipleChoiceMulti",
  NPS: "nps", CTA: "cta", Rating: "rating", Consent: "consent", PictureSelection: "pictureSelection", Cal: "cal", Date: "date",
  Matrix: "matrix", Address: "address", Ranking: "ranking", ContactInfo: "contactInfo", CSAT: "csat", CES: "ces",
};
// forma2 apps/web/lib/i18n/utils.ts getLocalizedValue
const isI18nObject = (value) => typeof value === "object" && value !== null && Object.keys(value).includes("default");
export const getLocalizedValue = (value, languageId) => {
  if (!value) return "";
  if (isI18nObject(value)) { if (value[languageId]) return value[languageId]; return ""; }
  return "";
};
// forma2 getTextContent for plain text (the fixtures carry no markup).
export const getTextContent = (str) => (!str || str.trim() === "" ? "" : str.trim());
export const convertFloatTo2Decimal = (num) => Math.round(num * 100) / 100;
export const getIngestedStorageKeys = (survey) => survey.embeddedFields.filter((f) => f.source === "ingested").map((f) => f.storageKey);
