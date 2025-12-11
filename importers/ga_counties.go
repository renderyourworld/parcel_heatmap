package importers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/renderyourworld/parcel_heatmap/models"
	"gorm.io/gorm"
)

const (
	// sagisEndpoint is the Georgia SAGIS API endpoint for county boundaries
	sagisEndpoint = "https://pub.sagis.org/arcgis/rest/services/OpenData/Boundaries/MapServer/15/query?where=OBJECTID_1=%d&f=geoJson&returnGeometry=true&outFields=NAME10,totpop10,Reg_Comm,Acres,Sq_Miles&outSR=4326"
	// maxCountyID is the number of counties in Georgia (159)
	maxCountyID = 159
	// maxRetries specifies how many times to retry a failed HTTP request
	maxRetries = 3
)

// StartCountyImporter fetches all Georgia county boundaries from the SAGIS API
// and inserts them into the database. It includes rate limiting (400ms between requests)
// and retry logic with exponential backoff to handle transient errors.
//
// The importer populates the counties table with boundary geometry and demographic data.
// Derived geometries (centroid, bbox, simplified boundary) and precomputed JSONB columns
// are automatically populated by database triggers on INSERT/UPDATE.
func StartCountyImporter(db *gorm.DB) error {

	log.Println("Starting rate-limited county import from SAGIS API...")

	for id := 1; id <= maxCountyID; id++ {
		// Rate limiting: 400ms between requests keeps us under 3 req/second limit
		time.Sleep(400 * time.Millisecond)

		var collection models.CountyFeatureCollection
		var feature models.CountyFeature
		isSuccessful := false

		// Retry logic for transient errors
		for attempt := 1; attempt <= maxRetries; attempt++ {
			// Build the request URL for the specific county
			url := fmt.Sprintf(sagisEndpoint, id)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
			resp, err := http.DefaultClient.Do(req)

			if err != nil {
				// ensure we cancel the context for this attempt
				cancel()
				log.Printf("Warning: Fetch error for ID %d (Attempt %d/%d): %v", id, attempt, maxRetries, err)
				if attempt < maxRetries {
					time.Sleep(1 * time.Second) // Wait before retrying
					continue
				}
				log.Printf("error: fetch failed after %d retries for ID %d. Skipping.", maxRetries, id)
				break
			}

			// Read response body while context is active, then cancel context
			bodyBytes, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()

			if readErr != nil {
				log.Printf("Warning: reading response body for ID %d (Attempt %d/%d): %v", id, attempt, maxRetries, readErr)
				if attempt < maxRetries {
					time.Sleep(1 * time.Second)
					continue
				}
				log.Printf("error: reading response body failed after %d retries for ID %d. Skipping.", maxRetries, id)
				break
			}

			// Check for non-200 responses
			if resp.StatusCode != http.StatusOK {
				log.Printf("Warning: API returned status %d for ID %d (Attempt %d/%d). Skipping.", resp.StatusCode, id, attempt, maxRetries)
				break
			}

			// Decode the GeoJSON response
			if err := json.Unmarshal(bodyBytes, &collection); err != nil {
				log.Printf("Warning: Decode failed for ID %d (Attempt %d/%d): %v", id, attempt, maxRetries, err)
				if attempt < maxRetries {
					time.Sleep(1 * time.Second)
					continue
				}
				log.Printf("error: decode failed after %d retries for ID %d. Skipping.", maxRetries, id)
				break
			}

			if len(collection.Features) == 1 {
				feature = collection.Features[0]
				isSuccessful = true
				break // Success! Exit retry loop
			} else {
				log.Printf("Warning: ID %d returned %d features. Skipping.", id, len(collection.Features))
				break
			}
		}

		if !isSuccessful {
			continue // Move to next county
		}

		// Convert GeoJSON feature to County model
		county, err := convertFeatureToCounty(feature)
		if err != nil {
			log.Printf("ERROR: Conversion failed for %s (ID %d): %v. Skipping.", feature.Properties.Name, id, err)
			continue
		}

		// Insert/update the county in the database
		if err := processCounty(db, county); err != nil {
			log.Printf("FATAL DB ERROR: Failed to process %s (ID %d): %v. Stopping import.", county.Name, id, err)
			return err
		}
	}

	log.Println("Sequential county import finished.")
	return nil
}

// convertFeatureToCounty transforms a GeoJSON county feature into a County model struct.
func convertFeatureToCounty(feature models.CountyFeature) (models.County, error) {
	props := feature.Properties

	// Marshall geometry to JSON string for PostGIS to process
	geomJSONString, err := json.Marshal(feature.Geometry)
	if err != nil {
		return models.County{}, fmt.Errorf("error marshalling geometry to JSON: %w", err)
	}

	county := models.County{
		Name:  props.Name,
		State: "GA",

		Boundary: string(geomJSONString),

		Population:  sql.NullInt64{Int64: int64(props.TotalPop), Valid: props.TotalPop > 0},
		Region:      sql.NullString{String: props.Region, Valid: props.Region != ""},
		Acres:       sql.NullFloat64{Float64: props.Acres, Valid: props.Acres > 0},
		SquareMiles: sql.NullFloat64{Float64: props.SqMiles, Valid: props.SqMiles > 0},

		GisApiUrl: sql.NullString{Valid: false},
		TaxApiUrl: sql.NullString{Valid: false},
	}

	return county, nil
}

// processCounty handles the insertion and PostGIS geometry calculations for a county.
func processCounty(db *gorm.DB, county models.County) error {
	log.Printf("[Worker] Starting processing county: %s", county.Name)

	// Insert or update county with boundary geometry
	// Note: boundary_geojson will be populated by the trigger on INSERT/UPDATE
	sql := `
		INSERT INTO counties (
            name, state, gis_api_url, tax_api_url,
            population, region, acres, square_miles,
            boundary, bbox, centroid, boundary_simplified, boundary_geojson
        ) VALUES (
            $1, $2, $3, $4, 
            $5, $6, $7, $8, 
            ST_SetSRID(ST_GeomFromGeoJSON($$` + county.Boundary + `$$), 4326), 
            NULL, NULL, NULL, NULL
        )
        -- Use ON CONFLICT (name) since 'name' is the unique identifier
        ON CONFLICT (name) DO UPDATE SET
            state = EXCLUDED.state,
            gis_api_url = EXCLUDED.gis_api_url,
            tax_api_url = EXCLUDED.tax_api_url,
            population = EXCLUDED.population,
            region = EXCLUDED.region,
            acres = EXCLUDED.acres,
            square_miles = EXCLUDED.square_miles,
            boundary = ST_SetSRID(ST_GeomFromGeoJSON($$` + county.Boundary + `$$), 4326),
            bbox = NULL, 
            centroid = NULL, 
            boundary_simplified = NULL,
            boundary_geojson = NULL;
    `

	// Execute the INSERT/UPDATE
	if err := db.Exec(
		sql,
		county.Name,
		county.State,
		county.GisApiUrl.String,
		county.TaxApiUrl.String,
		county.Population.Int64,
		county.Region.String,
		county.Acres.Float64,
		county.SquareMiles.Float64,
	).Error; err != nil {
		return fmt.Errorf("SQL INSERT/UPDATE failed for %s: %w", county.Name, err)
	}

	log.Printf("[Worker] Data inserted/updated successfully: %s", county.Name)

	// Calculate derived geometries: centroid, bbox, and simplified boundary
	// The database trigger will automatically refresh the precomputed JSONB columns
	if err := db.Exec(`
		UPDATE counties
		SET
			bbox = ST_Envelope(boundary),
			centroid = ST_Centroid(boundary),
			boundary_simplified = ST_Simplify(boundary, 0.005) -- Tolerance of ~500m
		WHERE name = ?`, county.Name).Error; err != nil {
		return fmt.Errorf("PostGIS calculation failed for %s: %w", county.Name, err)
	}

	log.Printf("[Worker] Geometry calculations complete: %s", county.Name)

	return nil
}
