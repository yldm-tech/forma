import { useAutoAnimate } from "@formkit/auto-animate/react";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { AnimatedSurveyBg } from "../../../features/survey-editor/components/animated-survey-bg";
import { ColorSurveyBg } from "../../../features/survey-editor/components/color-survey-bg";
import { UploadImageSurveyBg } from "../../../features/survey-editor/components/image-survey-bg";
import { ImageFromUnsplashSurveyBg } from "../../../features/survey-editor/components/unsplash-images";
import { TabBar } from "../tab-bar";
import { unsplashStatusQuery } from "./unsplash";

interface SurveyBgSelectorTabProps {
  handleBgChange: (bg: string, bgType: string) => void;
  colors: string[];
  bgType: string | null | undefined;
  bg: string;
}

export const SurveyBgSelectorTab = ({ handleBgChange, colors, bgType, bg }: SurveyBgSelectorTabProps) => {
  const [activeTab, setActiveTab] = useState(bgType || "color");
  const { t } = useTranslation();
  const [parent] = useAutoAnimate();
  const [colorBackground, setColorBackground] = useState(bg);
  const [animationBackground, setAnimationBackground] = useState(bg);
  const [uploadBackground, setUploadBackground] = useState(bg);
  const imageBackground = bgType === "image" ? bg : "";
  // The Unsplash tab is forma2's, offered only when the instance has UNSPLASH_ACCESS_KEY. A background already chosen from it keeps its tab either way, so the current image is shown rather than an empty panel.
  const { data: unsplash } = useQuery(unsplashStatusQuery());
  const isUnsplashConfigured = unsplash?.configured ?? false;

  const tabs = [
    { id: "color", label: t("workspace.surveys.edit.color") },
    { id: "animation", label: t("workspace.surveys.edit.animation") },
    { id: "upload", label: t("workspace.surveys.edit.upload") },
    { id: "image", label: t("workspace.surveys.edit.image") },
  ].filter((tab) => tab.id !== "image" || isUnsplashConfigured || bgType === "image");

  useEffect(() => {
    if (bgType === "color") {
      setColorBackground(bg);
      setAnimationBackground("");
      setUploadBackground("");
    }

    if (bgType === "animation") {
      setAnimationBackground(bg);
      setColorBackground("");
      setUploadBackground("");
    }

    if (bgType === "image") {
      setColorBackground("");
      setAnimationBackground("");
      setUploadBackground("");
    }

    if (bgType === "upload") {
      setUploadBackground(bg);
      setColorBackground("");
      setAnimationBackground("");
    }
  }, [bg, bgType]);

  const renderContent = () => {
    switch (activeTab) {
      case "color":
        return <ColorSurveyBg handleBgChange={handleBgChange} colors={colors} background={colorBackground} />;
      case "animation":
        return <AnimatedSurveyBg handleBgChange={handleBgChange} background={animationBackground} />;
      case "upload":
        return <UploadImageSurveyBg handleBgChange={handleBgChange} background={uploadBackground} />;
      case "image":
        if (isUnsplashConfigured) {
          return <ImageFromUnsplashSurveyBg handleBgChange={handleBgChange} background={imageBackground} />;
        }
        return imageBackground ? (
          <img
            src={imageBackground}
            alt={t("workspace.surveys.edit.image")}
            className="mt-2 aspect-[3/2] w-1/3 rounded-lg border-2 border-slate-800 object-cover"
          />
        ) : null;
      default:
        return null;
    }
  };

  return (
    <div className="mt-4 flex flex-col items-center justify-center rounded-lg">
      <TabBar
        tabs={tabs}
        activeId={activeTab}
        setActiveId={setActiveTab}
        tabStyle="button"
        className="bg-slate-100"
      />
      <div className="w-full rounded-b-lg border-x border-b border-slate-200 px-4 pt-2 pb-4" ref={parent}>
        {renderContent()}
      </div>
    </div>
  );
};
