package models

import (
	"encoding/json"
)

// Matches the top-level GeoJSON response
type CountyFeatureCollection struct {
	Features []CountyFeature `json:"features"`
}

// Matches the individual "feature" object
type CountyFeature struct {
	Geometry   Geometry                `json:"geometry"`
	Properties CountyFeatureProperties `json:"properties"`
}

// Matches the geometry block
type Geometry struct {
	Type        string          `json:"type"`        // "Polygon"
	Coordinates json.RawMessage `json:"coordinates"` // The nested coordinate arrays
}

// Matches the "properties" block
type CountyFeatureProperties struct {
	ObjectID int     `json:"OBJECTID_1"` // Not used, but good to know its mapping
	Name     string  `json:"NAME10"`     // County Name
	TotalPop int     `json:"totpop10"`   // Population
	Region   string  `json:"Reg_Comm"`   // Region
	Acres    float64 `json:"Acres"`      // Acres
	SqMiles  float64 `json:"Sq_Miles"`   // Square Miles
}

// ParcelFeatureCollection matches the top-level GeoJSON response for parcels
// Uses dynamic Properties map to handle varying field names across counties
type ParcelFeatureCollection struct {
	Type     string          `json:"type"`
	Features []ParcelFeature `json:"features"`
}

// ParcelFeature matches individual parcel feature with dynamic properties
type ParcelFeature struct {
	Type       string                 `json:"type"`
	Geometry   Geometry               `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}
