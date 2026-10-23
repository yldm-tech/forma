import { useInfiniteQuery } from "@tanstack/react-query";
import { CheckIcon, SearchIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  triggerUnsplashDownload,
  unsplashSearchQuery,
} from "../../../components/ui/background-styling-card/unsplash";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/input";
import { LoadingSpinner } from "../../../components/ui/loading-spinner";
import { cn } from "../../../lib/cn";
import { toast } from "../../../lib/toast";
import { useDebouncedValue } from "../../../lib/use-debounced-value";

interface ImageFromUnsplashSurveyBgProps {
  handleBgChange: (url: string, bgType: string) => void;
  /** The image background currently chosen, if any. */
  background: string;
}

interface ImageOption {
  id: string;
  altDescription: string;
  url: string;
  downloadLocation?: string;
  authorName?: string;
}

/** The library forma2 offered before a search: the files in web/public/image-backgrounds. */
export const defaultImages: ImageOption[] = [
  { id: "dog-1", altDescription: "Dog", url: "/image-backgrounds/dogs.webp" },
  { id: "pencil", altDescription: "Pencil", url: "/image-backgrounds/pencil.webp" },
  { id: "plant", altDescription: "Plant", url: "/image-backgrounds/plant.webp" },
  { id: "dog-2", altDescription: "Another Dog", url: "/image-backgrounds/dog-2.webp" },
  { id: "kitten-2", altDescription: "Another Kitten", url: "/image-backgrounds/kitten-2.webp" },
  { id: "lollipop", altDescription: "Lollipop", url: "/image-backgrounds/lolipop.webp" },
  { id: "oranges", altDescription: "Oranges", url: "/image-backgrounds/oranges.webp" },
  { id: "flower", altDescription: "Flower", url: "/image-backgrounds/flowers.webp" },
  { id: "supermario", altDescription: "Super Mario", url: "/image-backgrounds/supermario.webp" },
  { id: "shapes", altDescription: "Shapes", url: "/image-backgrounds/shapes.webp" },
  { id: "waves", altDescription: "Waves", url: "/image-backgrounds/waves.webp" },
  { id: "kitten-1", altDescription: "Kitten", url: "/image-backgrounds/kittens.webp" },
  { id: "windows", altDescription: "Windows", url: "/image-backgrounds/windows.webp" },
];

const errorMessage = (error: unknown) => (error instanceof Error ? error.message : String(error));

/** forma2's Unsplash picker: the default library, or the pages of an Unsplash search for what is typed. */
export const ImageFromUnsplashSurveyBg = ({ handleBgChange, background }: ImageFromUnsplashSurveyBgProps) => {
  const { t } = useTranslation();
  const inputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const searchQuery = useDebouncedValue(query.trim(), 500);
  const isSearching = query.trim() !== "";
  const search = useInfiniteQuery(unsplashSearchQuery(searchQuery));

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  useEffect(() => {
    if (search.error) toast.error(errorMessage(search.error));
  }, [search.error]);

  // While the typed text waits out the debounce, nothing is shown for the text the results belong to.
  const isCurrent = isSearching && searchQuery === query.trim();
  const images: ImageOption[] = !isSearching
    ? defaultImages
    : isCurrent
      ? (search.data?.pages.flatMap((page) => page.images) ?? [])
      : [];
  const isLoading = isSearching && (!isCurrent || search.isFetching);

  const handleImageSelected = async (image: ImageOption) => {
    handleBgChange(image.url, "image");
    if (!image.downloadLocation) return;
    try {
      await triggerUnsplashDownload(image.downloadLocation);
    } catch (error) {
      toast.error(errorMessage(error));
    }
  };

  return (
    <div className="relative mt-2 w-full">
      <div className="relative">
        <SearchIcon className="absolute top-1/2 left-2 h-6 w-4 -translate-y-1/2 text-slate-500" />
        <Input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={t("workspace.surveys.edit.try_lollipop_or_mountain")}
          className="pl-8"
          ref={inputRef}
          aria-label={t("workspace.surveys.edit.search_for_images")}
        />
      </div>
      <div className="relative mt-4 grid grid-cols-3 gap-1">
        {images.map((image) => {
          const isSelected = background === image.url;
          return (
            <button
              type="button"
              key={image.id}
              aria-pressed={isSelected}
              aria-label={image.altDescription || image.authorName || image.id}
              onClick={() => void handleImageSelected(image)}
              className={cn(
                "group relative aspect-[3/2] cursor-pointer overflow-hidden rounded-lg border-2",
                isSelected ? "border-slate-800" : "border-transparent"
              )}>
              <img src={image.url} alt="" loading="lazy" className="h-full w-full object-cover" />
              {image.authorName && (
                <span className="absolute right-1 bottom-1 hidden rounded-sm bg-black/75 px-2 py-1 text-xs text-white group-hover:block">
                  {image.authorName}
                </span>
              )}
              {isSelected && (
                <span className="absolute top-2 right-2 flex size-5 items-center justify-center rounded-full bg-slate-800 text-white shadow">
                  <CheckIcon className="size-3" strokeWidth={3} />
                </span>
              )}
            </button>
          );
        })}
        {isLoading && (
          <div className="col-span-3 flex items-center justify-center p-3">
            <LoadingSpinner />
          </div>
        )}
        {images.length > 0 && !isLoading && isSearching && search.hasNextPage && (
          <Button
            size="sm"
            variant="secondary"
            className="col-span-3 mt-3 flex items-center justify-center"
            type="button"
            data-testid="unsplash-select-button"
            onClick={() => void search.fetchNextPage()}>
            {t("common.load_more")}
          </Button>
        )}
        {!isLoading && images.length === 0 && isSearching && (
          <div className="col-span-3 flex items-center justify-center text-sm text-slate-500">
            {t("workspace.surveys.edit.no_images_found_for", { query: query.trim() })}
          </div>
        )}
      </div>
    </div>
  );
};
