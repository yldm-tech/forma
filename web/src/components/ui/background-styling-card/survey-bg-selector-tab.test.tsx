import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { i18n as I18n } from "i18next";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, expect, test, vi } from "vite-plus/test";
import { initI18n } from "../../../lib/i18n";
import { toast } from "../../../lib/toast";
import { SurveyBgSelectorTab } from "./survey-bg-selector-tab";

let i18n: I18n;

beforeAll(async () => {
  i18n = await initI18n("en-US");
});

afterEach(() => {
  cleanup();
  for (const shown of toast.getSnapshot()) toast.dismiss(shown.id);
  vi.unstubAllGlobals();
});

const photo = {
  id: "abc",
  altDescription: "a red lollipop",
  url: "https://images.unsplash.com/photo-1?ixid=x1&dpr=2&authorLink=https%3A%2F%2Funsplash.com%2F%40jane&authorName=Jane%20Doe&utm_source=forma&utm_medium=referral",
  downloadLocation: "https://api.unsplash.com/photos/abc/download?ixid=x1",
  authorName: "Jane Doe",
};

/** The Go server's /api/app/unsplash routes, with UNSPLASH_ACCESS_KEY set or not; every call is recorded. */
const stubServer = (configured: boolean) => {
  const fetchMock = vi.fn(async (path: string, init?: RequestInit) => {
    const json = (body: unknown) =>
      new Response(JSON.stringify(body), { headers: { "content-type": "application/json" } });
    if (path === "/api/app/unsplash/status") return json({ configured });
    if (path.startsWith("/api/app/unsplash/search?")) return json({ images: [photo] });
    if (path === "/api/app/unsplash/download" && init?.method === "POST")
      return new Response(null, { status: 204 });
    throw new Error(`unexpected ${init?.method ?? "GET"} ${path}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
};

const renderTab = (bgType: string, bg: string) => {
  const handleBgChange = vi.fn();
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <SurveyBgSelectorTab handleBgChange={handleBgChange} colors={["#ffffff"]} bgType={bgType} bg={bg} />
      </QueryClientProvider>
    </I18nextProvider>
  );
  return { handleBgChange, queryClient };
};

const statusLoaded = (fetchMock: ReturnType<typeof stubServer>) =>
  waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/app/unsplash/status", expect.anything()));

test("the image tab is hidden when Unsplash is not configured", async () => {
  const fetchMock = stubServer(false);
  renderTab("color", "#ffffff");
  await statusLoaded(fetchMock);
  expect(screen.getByRole("button", { name: "Upload" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Image" })).toBeNull();
});

test("the image tab offers the default library, then Unsplash results, and pings the download on select", async () => {
  const fetchMock = stubServer(true);
  const { handleBgChange } = renderTab("color", "#ffffff");
  fireEvent.click(await screen.findByRole("button", { name: "Image" }));

  // Before a search, forma2's library of /image-backgrounds.
  const dog = await screen.findByRole("button", { name: "Dog" });
  expect(dog.querySelector("img")?.getAttribute("src")).toBe("/image-backgrounds/dogs.webp");
  fireEvent.click(dog);
  expect(handleBgChange).toHaveBeenLastCalledWith("/image-backgrounds/dogs.webp", "image");

  fireEvent.change(screen.getByRole("textbox", { name: "Search for images" }), {
    target: { value: "lollipop" },
  });
  const result = await screen.findByRole("button", { name: "a red lollipop" }, { timeout: 3000 });
  expect(fetchMock).toHaveBeenCalledWith("/api/app/unsplash/search?query=lollipop&page=1", expect.anything());
  expect(screen.queryByRole("button", { name: "Dog" })).toBeNull();

  fireEvent.click(result);
  expect(handleBgChange).toHaveBeenLastCalledWith(photo.url, "image");
  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/app/unsplash/download",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ downloadUrl: photo.downloadLocation }),
      })
    )
  );
  // The default library has no download location to ping.
  expect(fetchMock.mock.calls.filter(([path]) => path === "/api/app/unsplash/download")).toHaveLength(1);
});

test("a survey with an image background opens on the image tab with it selected", async () => {
  stubServer(true);
  renderTab("image", "/image-backgrounds/pencil.webp");
  const pencil = await screen.findByRole("button", { name: "Pencil" });
  expect(pencil.getAttribute("aria-pressed")).toBe("true");
  expect(screen.getByRole("button", { name: "Dog" }).getAttribute("aria-pressed")).toBe("false");
});

test("without Unsplash an image background still shows on its tab", async () => {
  const fetchMock = stubServer(false);
  renderTab("image", photo.url);
  await statusLoaded(fetchMock);
  expect(screen.getByRole("button", { name: "Image" })).toBeTruthy();
  expect(screen.getByRole("img", { name: "Image" }).getAttribute("src")).toBe(photo.url);
  expect(screen.queryByRole("textbox", { name: "Search for images" })).toBeNull();
});
