package utils

// BoundingBox represents a geographic bounding box with JSON tags for API compatibility.
type BoundingBox struct {
	MinX float64 `json:"minx"`
	MinY float64 `json:"miny"`
	MaxX float64 `json:"maxx"`
	MaxY float64 `json:"maxy"`
}
