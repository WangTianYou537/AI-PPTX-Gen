package ppt

type ArchitectRequest struct {
	Config     any    `json:"config"`
	Topic      string `json:"topic"`
	Audience   string `json:"audience"`
	SlideCount int    `json:"slideCount"`
	Style      string `json:"style"`
}

type SlideOutline struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Purpose    string   `json:"purpose"`
	KeyPoints  []string `json:"keyPoints"`
	VisualHint string   `json:"visualHint"`
}

type PresentationOutline struct {
	Title    string         `json:"title"`
	Theme    string         `json:"theme"`
	Audience string         `json:"audience"`
	Style    string         `json:"style"`
	Slides   []SlideOutline `json:"slides"`
}

type SVGRequest struct {
	Config  any                 `json:"config"`
	Outline PresentationOutline `json:"outline"`
}

type SlideSVG struct {
	SlideID string `json:"slideId"`
	Title   string `json:"title"`
	SVG     string `json:"svg"`
}

type SVGResponse struct {
	Slides []SlideSVG `json:"slides"`
	Quota  any        `json:"quota,omitempty"`
}
