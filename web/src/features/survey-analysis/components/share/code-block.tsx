/** forma2's CodeBlock without highlighting: the code as the reader copies it. */
export const CodeBlock = ({ children, className }: { children: string; className?: string }) => (
  <pre
    className={`w-full overflow-x-auto rounded-md border border-slate-200 bg-white p-4 font-mono text-xs whitespace-pre text-slate-800 ${className ?? ""}`}>
    <code>{children}</code>
  </pre>
);
