import { redirect } from "next/navigation";

// The SDK connection is now a page inside Integrations, which is where its card always pointed. Kept so the links in the docs and anyone's bookmarks still land somewhere.
const Page = async (props: { params: Promise<{ workspaceId: string }> }) => {
  const { workspaceId } = await props.params;
  redirect(`/workspaces/${workspaceId}/integrations/app-connection`);
};

export default Page;
