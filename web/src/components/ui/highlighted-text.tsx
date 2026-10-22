import { highlightParts } from "../../lib/contacts";

/** forma2's HighlightedText: the value with each match of the search marked. */
export const HighlightedText = ({ value, searchValue }: { value: string; searchValue: string }) => (
  <span className="text-slate-900">
    {highlightParts(value, searchValue).map((part, index) =>
      part.match ? (
        // oxlint-disable-next-line react/no-array-index-key -- parts are positional
        <mark key={index} className="bg-[#FFFF00]">
          {part.text}
        </mark>
      ) : (
        // oxlint-disable-next-line react/no-array-index-key -- parts are positional
        <span key={index}>{part.text}</span>
      )
    )}
  </span>
);
