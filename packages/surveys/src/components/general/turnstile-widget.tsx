import { useEffect, useRef, useState } from "preact/hooks";
import type { TurnstileChallenge } from "@/lib/turnstile";
import { cn } from "@/lib/utils";

/**
 * Where Cloudflare Turnstile renders its widget for the survey (lib/turnstile.ts). It takes no room while Turnstile checks the respondent in the background, and shows the challenge, centred and no wider than the survey, only while Cloudflare needs the respondent to interact with it.
 */
export function TurnstileWidget({ challenge }: Readonly<{ challenge: TurnstileChallenge }>) {
  const container = useRef<HTMLDivElement>(null);
  const [isInteractive, setIsInteractive] = useState(false);

  useEffect(() => {
    if (container.current) void challenge.mount(container.current, setIsInteractive);
    return () => challenge.remove();
  }, [challenge]);

  return (
    <div
      ref={container}
      data-testid="forma-turnstile"
      className={cn("flex w-full max-w-full justify-center overflow-hidden", isInteractive ? "p-2" : "")}
    />
  );
}
