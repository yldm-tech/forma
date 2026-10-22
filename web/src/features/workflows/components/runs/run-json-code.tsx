import { useMemo } from "react";
import { stringifyRunJson } from "../../lib/run-display";

/** A run payload as bounded, pretty-printed JSON, stringified once per value. */
export const RunJsonCode = ({ value }: Readonly<{ value: unknown }>) => {
  const json = useMemo(() => stringifyRunJson(value), [value]);
  return (
    <pre className="w-full overflow-x-auto rounded-lg bg-slate-50 p-3 font-mono text-xs whitespace-pre-wrap text-slate-800">
      <code>{json}</code>
    </pre>
  );
};
