import { Outlet, createFileRoute } from "@tanstack/react-router";
import { FormWrapper } from "../../features/auth/components/ui";
import { useAttributionTracker } from "../../lib/attribution";

// Every /auth screen renders in the same card. Which of them a signed-in visitor may see is decided per screen: an emailed verification or recovery link must still work in a browser that has a session.
export const Route = createFileRoute("/auth")({
  component: AuthLayout,
});

function AuthLayout() {
  // forma2's auth layout recorded the first-touch marketing attribution on every auth screen.
  useAttributionTracker();
  return (
    <FormWrapper>
      <Outlet />
    </FormWrapper>
  );
}
