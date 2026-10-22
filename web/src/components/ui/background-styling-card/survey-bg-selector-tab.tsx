import { useAutoAnimate } from "@formkit/auto-animate/react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { AnimatedSurveyBg } from "../../../features/survey-editor/components/animated-survey-bg";
import { ColorSurveyBg } from "../../../features/survey-editor/components/color-survey-bg";
import { UploadImageSurveyBg } from "../../../features/survey-editor/components/image-survey-bg";
import { TabBar } from "../tab-bar";

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

  const tabs = [
    { id: "color", label: t("workspace.surveys.edit.color") },
    { id: "animation", label: t("workspace.surveys.edit.animation") },
    { id: "upload", label: t("workspace.surveys.edit.upload") },
  ];

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
