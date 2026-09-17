import { redirect } from "next/navigation";

// Catches the old sub-paths (webhooks, slack, notion, airtable, google-sheets) alongside the index redirect beside it.
const Page = async (props: { params: Promise<{ workspaceId: string; rest: string[] }> }) => {
  const { workspaceId, rest } = await props.params;
  redirect(`/workspaces/${workspaceId}/integrations/${rest.join("/")}`);
};

export default Page;
