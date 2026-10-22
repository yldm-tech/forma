import { FileInput } from "../../../components/ui/file-input/index";

interface UploadImageSurveyBgProps {
  handleBgChange: (url: string, bgType: string) => void;
  background: string;
}

export const UploadImageSurveyBg = ({ handleBgChange, background }: UploadImageSurveyBgProps) => {
  return (
    <div className="mt-2 w-full">
      <div className="flex w-full items-center justify-center">
        <FileInput
          id="survey-bg-file-input"
          onFileUpload={(url: string[] | undefined, _fileType: "image" | "video") => {
            const [first] = url ?? [];
            if (first) {
              handleBgChange(first, "upload");
            } else {
              handleBgChange("", "upload");
            }
          }}
          fileUrl={background}
        />
      </div>
    </div>
  );
};
