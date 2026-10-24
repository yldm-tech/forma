import { createFileRoute } from "@tanstack/react-router";
import { ProfilePage } from "../../../../features/account/profile";

export const Route = createFileRoute("/_authenticated/account/settings/profile")({
  component: ProfilePage,
});
