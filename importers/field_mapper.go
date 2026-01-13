package importers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/renderyourworld/parcel_heatmap/models"
	"gorm.io/gorm"
)

// Applies simple string transformations based on transform column
func applyTransform(value string, transform sql.NullString) string {
	if !transform.Valid || transform.String == "" {
		return value
	}

	transformStr := strings.TrimSpace(transform.String)

	// Handle REPLACE(old,new)
	if strings.HasPrefix(transformStr, "REPLACE(") && strings.HasSuffix(transformStr, ")") {
		// Extract parameters from REPLACE(old,new)
		params := transformStr[8 : len(transformStr)-1] // Remove "REPLACE(" and ")"
		parts := strings.Split(params, ",")
		if len(parts) == 2 {
			oldStr := strings.Trim(parts[0], " '\"")
			newStr := strings.Trim(parts[1], " '\"")
			value = strings.ReplaceAll(value, oldStr, newStr)
		}
	}

	// Handle TRIM()
	if transformStr == "TRIM()" || transformStr == "TRIM" {
		value = strings.TrimSpace(value)
	}

	// Handle UPPER()
	if transformStr == "UPPER()" || transformStr == "UPPER" {
		value = strings.ToUpper(value)
	}

	// Handle LOWER()
	if transformStr == "LOWER()" || transformStr == "LOWER" {
		value = strings.ToLower(value)
	}

	// Handle CONCAT_ADDRESS - formats address fields into "street, city, state, zip"
	if transformStr == "CONCAT_ADDRESS()" || transformStr == "CONCAT_ADDRESS" {
		// Value comes in as pipe-separated: "street|city|state|zip"
		parts := strings.Split(value, "|")
		if len(parts) >= 4 {
			street := strings.TrimSpace(parts[0])
			city := strings.TrimSpace(parts[1])
			state := strings.TrimSpace(parts[2])
			zip := strings.TrimSpace(parts[3])

			// Format as "street, city, state zip"
			if street != "" && city != "" {
				value = fmt.Sprintf("%s, %s, %s %s", street, city, state, zip)
			} else if city != "" {
				value = fmt.Sprintf("%s, %s %s", city, state, zip)
			} else {
				value = fmt.Sprintf("%s %s", state, zip)
			}
		}
	}

	// Handle CONCAT - joins multiple fields with a space
	if transformStr == "CONCAT()" || transformStr == "CONCAT" {
		// Value comes in as pipe-separated if from multiple fields
		parts := strings.Split(value, "|")
		var nonEmpty []string
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				nonEmpty = append(nonEmpty, trimmed)
			}
		}
		value = strings.Join(nonEmpty, " ")
	}

	// Handle EXTRACT_SITE_NUMBER - pulls the first part of an address (e.g., "4330" from "4330 COVERED BRIDGE RD")
	if transformStr == "EXTRACT_SITE_NUMBER()" || transformStr == "EXTRACT_SITE_NUMBER" {
		parts := strings.Fields(value)
		if len(parts) > 0 {
			value = parts[0]
		}
	}

	// CALC_FROM_GEOMETRY is handled specially in mapFeatureToParcel, not here.
	// When source_field="$GEOMETRY" and transform="CALC_FROM_GEOMETRY",
	// the acres will be calculated from the parcel geometry via PostGIS in the INSERT.

	return value
}

// Assigns a value to the appropriate parcel field based on target column
func assignFieldValue(parcel *models.Parcel, targetColumn, strValue, sourceField string) {
	switch targetColumn {
	case "parcel_id":
		parcel.ParcelID = strValue
	case "objectid":
		if intVal, err := strconv.ParseInt(strValue, 10, 64); err == nil {
			parcel.ObjectID = sql.NullInt64{Int64: intVal, Valid: true}
		}
	case "site_address":
		parcel.SiteAddress = sql.NullString{String: strValue, Valid: strValue != ""}
	case "site_number":
		parcel.SiteNumber = sql.NullString{String: strValue, Valid: strValue != ""}
	case "owner_name":
		parcel.OwnerName = sql.NullString{String: strValue, Valid: strValue != ""}
	case "owner_address":
		parcel.OwnerAddress = sql.NullString{String: strValue, Valid: strValue != ""}
	case "acres":
		if val, err := strconv.ParseFloat(strValue, 64); err == nil {
			var acres float64

			// If the source field starts with "shape", assume it's sq ft and convert to acres (43560 sqft per acre)
			if strings.HasPrefix(strings.ToLower(sourceField), "shape") {
				acres = val / 43560.0
			} else {
				// Otherwise assume it's already in acres
				acres = val
			}

			// Round to 2 decimal places
			acres = float64(int(acres*100+0.5)) / 100
			parcel.Acres = sql.NullFloat64{Float64: acres, Valid: true}
		}
	case "classification":
		parcel.Classification = sql.NullString{String: strValue, Valid: strValue != ""}
	case "tax_district":
		parcel.TaxDistrict = sql.NullString{String: strValue, Valid: strValue != ""}
	}
}

// Converts an interface{} value to string representation
func convertToString(rawValue interface{}) string {
	switch v := rawValue.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// Retrieves field mappings for a county from the database
func loadFieldMappings(db *gorm.DB, countyID uint16) ([]models.CountyFieldMapping, error) {
	var mappings []models.CountyFieldMapping

	if err := db.Where("county_id = ?", countyID).Find(&mappings).Error; err != nil {
		return nil, fmt.Errorf("error loading field mappings: %w", err)
	}

	if len(mappings) == 0 {
		return nil, fmt.Errorf("no field mappings found for county_id %d - county not configured", countyID)
	}

	return mappings, nil
}

// Transforms a GeoJSON feature into a Parcel model using field mappings
func mapFeatureToParcel(properties map[string]interface{}, geometry models.Geometry, mappings []models.CountyFieldMapping, countyID uint16) (*models.Parcel, string) {
	parcel := &models.Parcel{
		CountyID: countyID,
		// Processed defaults to NULL (success), only set to FALSE on error
	}

	// Marshal geometry to JSON string for PostGIS
	geomJSON, err := json.Marshal(geometry)
	if err != nil {
		return parcel, fmt.Sprintf("failed to marshal geometry: %v", err)
	}
	parcel.Geometry = string(geomJSON)

	// Track if parcel_id was found (required field)
	parcelIDFound := false

	// Iterate through field mappings and extract values
	for _, mapping := range mappings {
		// Handle special $GEOMETRY source field for calculating values from geometry
		if mapping.SourceField == "$GEOMETRY" {
			if mapping.Transform.Valid && mapping.Transform.String == "CALC_FROM_GEOMETRY" {
				// Mark that acres should be calculated from geometry in PostgreSQL
				// We leave parcel.Acres as NULL; insertParcel will use COALESCE with ST_Area
				if mapping.TargetColumn == "acres" {
					parcel.CalcAcresFromGeometry = true
				}
			}
			continue
		}

		// Handle multi-field source (comma-separated fields for complex mappings)
		sourceFields := strings.Split(mapping.SourceField, ",")
		var strValue string

		if len(sourceFields) > 1 {
			// Multi-field mapping: collect values from multiple source fields
			var parts []string
			for _, srcField := range sourceFields {
				srcField = strings.TrimSpace(srcField)
				if val, exists := properties[srcField]; exists && val != nil {
					parts = append(parts, convertToString(val))
				}
			}

			if len(parts) == 0 {
				// All fields are missing/null - leave as zero value (NULL)
				continue
			}

			// Apply transform to format multi-field data
			strValue = applyTransform(strings.Join(parts, "|"), mapping.Transform)
		} else {
			// Single field mapping
			rawValue, exists := properties[mapping.SourceField]
			if !exists || rawValue == nil {
				// Field doesn't exist or is null - leave as zero value (NULL)
				continue
			}

			// Convert to string
			strValue = convertToString(rawValue)

			// Apply transform if specified
			strValue = applyTransform(strValue, mapping.Transform)
		}

		// Convert to target type and assign to parcel field
		assignFieldValue(parcel, mapping.TargetColumn, strValue, mapping.SourceField)

		// Track if parcel_id was successfully assigned
		if mapping.TargetColumn == "parcel_id" {
			parcelIDFound = true
		}
	}

	// Validate required fields
	if !parcelIDFound || parcel.ParcelID == "" {
		parcel.Processed = sql.NullBool{Bool: false, Valid: true} // FALSE = error
		return parcel, "missing required field: parcel_id"
	}

	// Leave processed as NULL (zero value) = success
	return parcel, ""
}
