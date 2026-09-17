import { redirect } from "next/navigation";

// Tags moved out of settings and into the sidebar. Kept so the links in the docs and anyone's bookmarks still land somewhere.
const Page = async (props: { params: Promise<{ workspaceId: string }> }) => {
  const { workspaceId } = await props.params;
  redirect(`/workspaces/${workspaceId}/tags`);
};

export default Page;
