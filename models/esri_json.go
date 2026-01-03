package models

import (
	"encoding/json"
)

// EsriJSONFeatureCollection matches the response from ArcGIS when f=json
type EsriJSONFeatureCollection struct {
	Features []EsriJSONFeature `json:"features"`
}

// EsriJSONFeature matches individual feature in Esri JSON format
type EsriJSONFeature struct {
	Attributes map[string]interface{} `json:"attributes"`
	Geometry   EsriJSONGeometry       `json:"geometry"`
}

// EsriJSONGeometry matches the geometry object in Esri JSON
type EsriJSONGeometry struct {
	Rings [][][]float64 `json:"rings"` // For Polygons
	X     float64       `json:"x"`     // For Points
	Y     float64       `json:"y"`     // For Points
}

// ToGeoJSONGeometry converts Esri geometry to GeoJSON-compatible Geometry model
func (e EsriJSONGeometry) ToGeoJSONGeometry() Geometry {
	// Handle Polygon (most common for parcels)
	if len(e.Rings) > 0 {
		// Esri "rings" structure matches GeoJSON "Polygon" coordinates structure:
		// [ [ [x,y], [x,y] ... ], [ ... ] ]

		// Note: This assumes the rings constitute a single Polygon (outer + holes).
		// If Esri returns disjoint rings (MultiPolygon), this simple mapping treats them
		// as a single Polygon with multiple rings, which might be topologically invalid
		// in strict GeoJSON but often handled by robust parsers or PostGIS.

		coords, _ := json.Marshal(e.Rings)
		return Geometry{
			Type:        "Polygon",
			Coordinates: coords,
		}
	}

	// Handle Point (fallback)
	if e.X != 0 || e.Y != 0 {
		coords, _ := json.Marshal([]float64{e.X, e.Y})
		return Geometry{
			Type:        "Point",
			Coordinates: coords,
		}
	}

	return Geometry{Type: "Unknown", Coordinates: []byte("[]")}
}
