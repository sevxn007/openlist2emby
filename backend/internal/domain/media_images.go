package domain

// MediaImageQuery uses the movie or series ID; TV children are addressed by number.
type MediaImageQuery struct {
	Source    string
	Type      string
	ID        int64
	ImageType string
	Season    *int
	Episode   *int
}

type MediaImage struct {
	Source       string `json:"source"`
	ImageType    string `json:"image_type"`
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	Language     string `json:"language,omitempty"`
}
