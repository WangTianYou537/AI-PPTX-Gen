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
	// VisualDesign is optional deck-wide palette produced by theme planner.
	VisualDesign *VisualDesignLock `json:"visualDesign,omitempty"`
}

type SVGRequest struct {
	Config  any                 `json:"config"`
	Outline PresentationOutline `json:"outline"`
}

type SlideSVG struct {
	SlideID string `json:"slideId"`
	Title   string `json:"title"`
	SVG     string `json:"svg"`
	// Error is set when this slide failed generation; SVG may be empty.
	Error string `json:"error,omitempty"`
}

type SVGResponse struct {
	Slides []SlideSVG `json:"slides"`
	// Failed is the number of slides that failed generation.
	Failed int `json:"failed,omitempty"`
	Quota  any `json:"quota,omitempty"`
}
