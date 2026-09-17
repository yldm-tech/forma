import { redirect } from "next/navigation";

// User actions moved out of settings and into the sidebar. Kept so the links in the docs and anyone's bookmarks still land somewhere.
const Page = async (props: { params: Promise<{ workspaceId: string }> }) => {
  const { workspaceId } = await props.params;
  redirect(`/workspaces/${workspaceId}/user-actions`);
};

export default Page;
