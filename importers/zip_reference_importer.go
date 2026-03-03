package importers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

type zipAreaFeatureCollection struct {
	Features []zipAreaFeature `json:"features"`
}

type zipAreaFeature struct {
	Type       string                 `json:"type"`
	Geometry   map[string]interface{} `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}

var nonDigit = regexp.MustCompile(`\D`)

const (
	censusZCTAQueryURL = "https://tigerweb.geo.census.gov/arcgis/rest/services/TIGERweb/tigerWMS_ACS2025/MapServer/2/query"
	zippopotamURL      = "https://api.zippopotam.us/us/%s"
)

// EnsureZipReferenceData bootstraps ZIP polygons and ZIP->city lookup data so
// parcel_search can be built from a single CLI entry point.
func EnsureZipReferenceData(gormDB *gorm.DB) error {
	if err := ensureZipAreas(gormDB); err != nil {
		return err
	}
	if err := ensureZipCityLookup(gormDB); err != nil {
		return err
	}
	return nil
}

func ensureZipAreas(gormDB *gorm.DB) error {
	var count int64
	if err := gormDB.Raw(`SELECT COUNT(*) FROM us_zip5_areas`).Scan(&count).Error; err != nil {
		return fmt.Errorf("failed checking us_zip5_areas count: %w", err)
	}
	if count > 0 {
		log.Printf("ZIP areas present (%d rows), skipping download.", count)
		return nil
	}

	log.Printf("ZIP areas table empty. Downloading GA ZCTA polygons from Census TIGERweb...")
	if err := syncGAZipAreasFromCensus(gormDB); err != nil {
		return fmt.Errorf("failed syncing GA ZCTA polygons: %w", err)
	}
	return nil
}

func syncGAZipAreasFromCensus(gormDB *gorm.DB) error {
	client := &http.Client{Timeout: 60 * time.Second}
	offset := 0
	pageSize := 100
	totalUpserted := 0

	for {
		collection, err := fetchCensusZCTAPage(client, offset, pageSize)
		if err != nil {
			return err
		}
		if len(collection.Features) == 0 {
			break
		}

		tx := gormDB.Begin()
		if tx.Error != nil {
			return fmt.Errorf("failed beginning tx for ZCTA import: %w", tx.Error)
		}

		for i, f := range collection.Features {
			zip5 := normalizeZip5(extractZipFromProps(f.Properties))
			if zip5 == "" || f.Geometry == nil {
				continue
			}

			geomJSON, err := json.Marshal(f.Geometry)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("failed marshalling ZCTA geometry at offset=%d index=%d: %w", offset, i, err)
			}

			if err := tx.Exec(`
				INSERT INTO us_zip5_areas (zip5, geom, updated_at)
				VALUES (?, ST_SetSRID(ST_GeomFromGeoJSON(?), 4326), NOW())
				ON CONFLICT (zip5) DO UPDATE SET
					geom = EXCLUDED.geom,
					updated_at = NOW()
			`, zip5, string(geomJSON)).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed upserting census ZCTA zip=%s: %w", zip5, err)
			}
			totalUpserted++
		}

		if err := tx.Commit().Error; err != nil {
			return fmt.Errorf("failed committing ZCTA import page offset=%d: %w", offset, err)
		}

		log.Printf("Census ZCTA import progress: upserted=%d page_size=%d offset=%d", totalUpserted, len(collection.Features), offset)
		if len(collection.Features) < pageSize {
			break
		}
		offset += pageSize
		time.Sleep(300 * time.Millisecond)
	}

	if totalUpserted == 0 {
		return fmt.Errorf("no ZCTA polygons imported from Census")
	}
	log.Printf("Census ZCTA import complete: upserted=%d", totalUpserted)
	return nil
}

func fetchCensusZCTAPage(client *http.Client, offset, pageSize int) (*zipAreaFeatureCollection, error) {
	params := url.Values{}
	params.Set("where", "1=1")
	params.Set("outFields", "ZCTA5,GEOID")
	params.Set("returnGeometry", "true")
	params.Set("f", "geojson")
	params.Set("outSR", "4326")
	params.Set("inSR", "4326")
	params.Set("resultOffset", strconv.Itoa(offset))
	params.Set("resultRecordCount", strconv.Itoa(pageSize))

	endpoint := censusZCTAQueryURL + "?" + params.Encode()
	resp, err := client.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed requesting census zcta page offset=%d: %w", offset, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed reading census zcta response offset=%d: %w", offset, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("census zcta request failed offset=%d status=%d body=%s", offset, resp.StatusCode, safeSnippet(body, 400))
	}

	var collection zipAreaFeatureCollection
	if err := json.Unmarshal(body, &collection); err != nil {
		return nil, fmt.Errorf("failed decoding census zcta page offset=%d: %w body=%s", offset, err, safeSnippet(body, 400))
	}
	return &collection, nil
}

func ensureZipCityLookup(gormDB *gorm.DB) error {
	var zipList []string
	if err := gormDB.Raw(`
		WITH ga_boundary AS (
			SELECT ST_Union(c.boundary) AS geom
			FROM counties c
			WHERE c.state = 'GA'
			  AND c.boundary IS NOT NULL
		)
		SELECT DISTINCT z.zip5
		FROM us_zip5_areas z
		CROSS JOIN ga_boundary g
		WHERE z.zip5 IS NOT NULL
		  AND z.zip5 <> ''
		  AND g.geom IS NOT NULL
		  AND z.geom && g.geom
		  AND ST_Intersects(z.geom, g.geom)
		ORDER BY z.zip5
	`).Scan(&zipList).Error; err != nil {
		return fmt.Errorf("failed reading ZIP list for city lookup: %w", err)
	}
	if len(zipList) == 0 {
		return fmt.Errorf("no ZIP polygons available for city lookup")
	}
	log.Printf("ZIP city lookup candidate ZIPs in Georgia footprint: %d", len(zipList))

	client := &http.Client{Timeout: 15 * time.Second}
	inserted := 0
	skipped := 0

	for i, zip5 := range zipList {
		var exists int64
		if err := gormDB.Raw(`
			SELECT COUNT(*)
			FROM us_zip5_city_lookup
			WHERE zip5 = ?
			  AND state = 'GA'
		`, zip5).Scan(&exists).Error; err != nil {
			return fmt.Errorf("failed checking ZIP city lookup for %s: %w", zip5, err)
		}
		if exists > 0 {
			skipped++
			continue
		}

		city, state, err := fetchCityForZip(client, zip5)
		if err != nil {
			log.Printf("ZIP city lookup warning zip=%s: %v", zip5, err)
			continue
		}
		if state != "GA" || city == "" {
			continue
		}

		city = titleWords(city)
		if err := gormDB.Exec(`
			INSERT INTO us_zip5_city_lookup (zip5, city, state, is_preferred, updated_at)
			VALUES (?, ?, 'GA', true, NOW())
			ON CONFLICT (zip5, city, state) DO UPDATE SET
				is_preferred = EXCLUDED.is_preferred,
				updated_at = NOW()
		`, zip5, city).Error; err != nil {
			return fmt.Errorf("failed upserting ZIP city zip=%s city=%s: %w", zip5, city, err)
		}

		inserted++
		if (i+1)%100 == 0 {
			log.Printf("ZIP city sync progress: processed=%d/%d inserted=%d skipped_existing=%d", i+1, len(zipList), inserted, skipped)
		}
		time.Sleep(40 * time.Millisecond)
	}

	log.Printf("ZIP city sync complete: inserted=%d skipped_existing=%d total_zip=%d", inserted, skipped, len(zipList))
	return nil
}

func fetchCityForZip(client *http.Client, zip5 string) (string, string, error) {
	resp, err := client.Get(fmt.Sprintf(zippopotamURL, zip5))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", "", fmt.Errorf("zip not found")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Places []struct {
			PlaceName string `json:"place name"`
			StateAbbr string `json:"state abbreviation"`
		} `json:"places"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", err
	}
	if len(payload.Places) == 0 {
		return "", "", fmt.Errorf("empty places payload")
	}
	return strings.TrimSpace(payload.Places[0].PlaceName), strings.ToUpper(strings.TrimSpace(payload.Places[0].StateAbbr)), nil
}

// StartZipReferenceImporter loads ZIP polygon and ZIP->city reference tables
// used by parcel_search enrichment. Inputs are local files to keep the pipeline
// deterministic and offline-friendly once datasets are downloaded.
func StartZipReferenceImporter(gormDB *gorm.DB, zipAreasGeoJSONPath, zipCityCSVPath string, replaceExisting bool) error {
	if strings.TrimSpace(zipAreasGeoJSONPath) == "" && strings.TrimSpace(zipCityCSVPath) == "" {
		return fmt.Errorf("at least one of --zip-areas-geojson or --zip-city-csv is required")
	}

	if replaceExisting {
		if err := truncateZipReferenceTables(gormDB); err != nil {
			return err
		}
	}

	if strings.TrimSpace(zipAreasGeoJSONPath) != "" {
		if err := importZipAreasGeoJSON(gormDB, zipAreasGeoJSONPath); err != nil {
			return err
		}
	}

	if strings.TrimSpace(zipCityCSVPath) != "" {
		if err := importZipCityCSV(gormDB, zipCityCSVPath); err != nil {
			return err
		}
	}

	return nil
}

func truncateZipReferenceTables(gormDB *gorm.DB) error {
	log.Printf("Clearing existing ZIP reference tables...")
	if err := gormDB.Exec(`TRUNCATE TABLE us_zip5_city_lookup`).Error; err != nil {
		return fmt.Errorf("failed truncating us_zip5_city_lookup: %w", err)
	}
	if err := gormDB.Exec(`TRUNCATE TABLE us_zip5_areas`).Error; err != nil {
		return fmt.Errorf("failed truncating us_zip5_areas: %w", err)
	}
	return nil
}

func importZipAreasGeoJSON(gormDB *gorm.DB, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed opening zip areas geojson '%s': %w", path, err)
	}
	defer file.Close()

	var collection zipAreaFeatureCollection
	if err := json.NewDecoder(file).Decode(&collection); err != nil {
		return fmt.Errorf("failed decoding zip areas geojson '%s': %w", path, err)
	}
	if len(collection.Features) == 0 {
		return fmt.Errorf("zip areas geojson '%s' has no features", path)
	}

	tx := gormDB.Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction for zip areas import: %w", tx.Error)
	}

	inserted := 0
	skipped := 0
	for i, f := range collection.Features {
		zip5 := normalizeZip5(extractZipFromProps(f.Properties))
		if zip5 == "" {
			skipped++
			continue
		}
		if f.Geometry == nil {
			skipped++
			continue
		}

		geomJSON, err := json.Marshal(f.Geometry)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed marshalling geometry for feature %d: %w", i, err)
		}

		if err := tx.Exec(`
			INSERT INTO us_zip5_areas (zip5, geom, updated_at)
			VALUES (?, ST_SetSRID(ST_GeomFromGeoJSON(?), 4326), NOW())
			ON CONFLICT (zip5) DO UPDATE SET
				geom = EXCLUDED.geom,
				updated_at = NOW()
		`, zip5, string(geomJSON)).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed upserting zip area %s: %w", zip5, err)
		}

		inserted++
		if inserted > 0 && inserted%1000 == 0 {
			log.Printf("ZIP areas import progress: %d/%d", inserted, len(collection.Features))
		}
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed committing zip areas import: %w", err)
	}

	log.Printf("ZIP areas import complete: inserted_or_updated=%d skipped=%d total_features=%d", inserted, skipped, len(collection.Features))
	return nil
}

func importZipCityCSV(gormDB *gorm.DB, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed opening zip city csv '%s': %w", path, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	rows, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("failed reading zip city csv '%s': %w", path, err)
	}
	if len(rows) < 2 {
		return fmt.Errorf("zip city csv '%s' must include header and at least one row", path)
	}

	header := normalizeHeader(rows[0])
	zipIdx, cityIdx, stateIdx, preferredIdx := detectZipCityColumns(header)
	if zipIdx < 0 || cityIdx < 0 {
		return fmt.Errorf("zip city csv '%s' requires ZIP and city columns", path)
	}

	tx := gormDB.Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction for zip city import: %w", tx.Error)
	}

	inserted := 0
	skipped := 0

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) == 0 {
			skipped++
			continue
		}

		zip5 := normalizeZip5(valueAt(row, zipIdx))
		city := strings.TrimSpace(valueAt(row, cityIdx))
		state := strings.ToUpper(strings.TrimSpace(valueAt(row, stateIdx)))
		if state == "" {
			state = "GA"
		}

		if zip5 == "" || city == "" {
			skipped++
			continue
		}

		isPreferred := parsePreferred(valueAt(row, preferredIdx))
		city = titleWords(strings.Join(strings.Fields(city), " "))

		if err := tx.Exec(`
			INSERT INTO us_zip5_city_lookup (zip5, city, state, is_preferred, updated_at)
			VALUES (?, ?, ?, ?, NOW())
			ON CONFLICT (zip5, city, state) DO UPDATE SET
				is_preferred = EXCLUDED.is_preferred,
				updated_at = NOW()
		`, zip5, city, state, isPreferred).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed upserting zip city row %d (zip=%s city=%s): %w", i+1, zip5, city, err)
		}

		inserted++
		if inserted > 0 && inserted%10000 == 0 {
			log.Printf("ZIP city import progress: %d/%d", inserted, len(rows)-1)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed committing zip city import: %w", err)
	}

	log.Printf("ZIP city import complete: inserted_or_updated=%d skipped=%d total_rows=%d at=%s", inserted, skipped, len(rows)-1, time.Now().UTC().Format(time.RFC3339))
	return nil
}

func extractZipFromProps(props map[string]interface{}) string {
	if props == nil {
		return ""
	}

	candidates := []string{"zip5", "ZIP5", "zcta5", "ZCTA5", "zcta5ce20", "ZCTA5CE20", "zcta5ce10", "ZCTA5CE10", "geoid20", "GEOID20", "geoid10", "GEOID10"}
	for _, key := range candidates {
		if v, ok := props[key]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func normalizeZip5(raw string) string {
	clean := nonDigit.ReplaceAllString(strings.TrimSpace(raw), "")
	if len(clean) == 9 {
		clean = clean[:5]
	}
	if len(clean) != 5 {
		return ""
	}
	return clean
}

func normalizeHeader(raw []string) []string {
	out := make([]string, len(raw))
	for i, h := range raw {
		h = strings.ToLower(strings.TrimSpace(h))
		h = strings.ReplaceAll(h, "_", "")
		h = strings.ReplaceAll(h, "-", "")
		h = strings.ReplaceAll(h, " ", "")
		out[i] = h
	}
	return out
}

func detectZipCityColumns(header []string) (zipIdx, cityIdx, stateIdx, preferredIdx int) {
	zipIdx, cityIdx, stateIdx, preferredIdx = -1, -1, -1, -1
	for i, h := range header {
		switch h {
		case "zip", "zipcode", "zip5", "zcta5":
			zipIdx = i
		case "city", "cityname", "preferredcity", "uspscity":
			cityIdx = i
		case "state", "stateabbr":
			stateIdx = i
		case "ispreferred", "preferred", "primary", "defaultcity":
			preferredIdx = i
		}
	}
	return
}

func valueAt(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return row[idx]
}

func parsePreferred(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return false
	}
	if v == "y" || v == "yes" || v == "true" || v == "t" {
		return true
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n > 0
	}
	return false
}

func titleWords(v string) string {
	parts := strings.Fields(strings.ToLower(strings.TrimSpace(v)))
	for i := range parts {
		if len(parts[i]) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, " ")
}

func safeSnippet(b []byte, max int) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) > max {
		b = b[:max]
	}
	s := strings.TrimSpace(string(b))
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
