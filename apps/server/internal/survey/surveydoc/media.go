package surveydoc

import (
	"fmt"
	"strings"
)

// invalidImageHint is forma2's INVALID_IMAGE_HINT.
var invalidImageHint = fmt.Sprintf("The URL must end in a supported image extension (%s). For CDN or extension-less links, upload the image to Forma or use a direct image URL.", strings.Join(ImageFileExtensions, ", "))

// MediaIssue is forma2's checkForInvalidMediaInBlocks: the first element image that is not an image file, element video that is not YouTube, Vimeo or Loom, or picture choice that is not an image file, or "".
func MediaIssue(blocks []Block) string {
	for b, block := range blocks {
		for e, el := range block.Elements {
			if el.ImageURL != nil && *el.ImageURL != "" && !IsValidImageFile(*el.ImageURL) {
				return fmt.Sprintf(`Invalid image URL in question %d of block "%s" (block %d). %s`, e+1, block.Name, b+1, invalidImageHint)
			}
			if el.VideoURL != nil && *el.VideoURL != "" && !IsValidVideoURL(*el.VideoURL) {
				return fmt.Sprintf(`Invalid video URL in question %d of block "%s" (block %d). Only YouTube, Vimeo, and Loom URLs are supported.`, e+1, block.Name, b+1)
			}
			if el.Type != ElementPictureSelection {
				continue
			}
			for c, choice := range el.Choices {
				if choice.ImageURL != "" && !IsValidImageFile(choice.ImageURL) {
					return fmt.Sprintf(`Invalid image URL in choice %d of question %d of block "%s". %s`, c+1, e+1, block.Name, invalidImageHint)
				}
			}
		}
	}
	return ""
}
