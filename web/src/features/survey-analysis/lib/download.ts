import type { DownloadedFile } from "../../../lib/api";
import type { ExportFormat } from "../api";

/** The name an export is saved under (forma2's downloadResponsesFile): the server's name, else a dated fallback, with the format's extension forced. */
export const exportFileName = (
  filename: string | null,
  format: ExportFormat,
  now: Date = new Date()
): string => {
  const extension = format === "xlsx" ? ".xlsx" : ".csv";
  const trimmed = (filename ?? "").trim();
  const name = trimmed || `responses-${now.toISOString().slice(0, 10)}${extension}`;
  return name.toLowerCase().endsWith(extension) ? name : `${name}${extension}`;
};

/** Hands a downloaded export to the browser as a file. The server already wrote the CSV's BOM, which Excel needs to read it as UTF-8, so the bytes are saved as they came. */
export const saveExport = (file: DownloadedFile, format: ExportFormat): void => {
  const url = URL.createObjectURL(file.blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = exportFileName(file.filename, format);
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
};
