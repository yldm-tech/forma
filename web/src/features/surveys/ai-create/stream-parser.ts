/**
 * forma2's NdjsonParser: an incremental NDJSON reader. A network chunk has nothing to do with a line boundary, so one object can straddle two chunks and two objects can arrive in one; keeping the split a pure string function makes those cases testable without a stream.
 */
export class NdjsonParser<T> {
  private buffer = "";

  /** Complete objects contained in this chunk. A partial trailing line is held for the next call. */
  push(chunk: string): T[] {
    this.buffer += chunk;
    const lines = this.buffer.split("\n");
    // The last element is whatever came after the final newline: either "" or a partial line.
    this.buffer = lines.pop() ?? "";
    return lines.map((line) => parseLine<T>(line)).filter((value): value is T => value !== null);
  }

  /** Whatever is left when the stream ends without a trailing newline. */
  flush(): T[] {
    const remaining = this.buffer;
    this.buffer = "";
    const parsed = parseLine<T>(remaining);
    return parsed === null ? [] : [parsed];
  }
}

/** Null rather than a throw on a malformed line: one unreadable frame costs the user one skipped snapshot, not the whole generation. */
function parseLine<T>(line: string): T | null {
  const trimmed = line.trim();
  if (trimmed.length === 0) return null;
  try {
    return JSON.parse(trimmed) as T;
  } catch {
    return null;
  }
}
