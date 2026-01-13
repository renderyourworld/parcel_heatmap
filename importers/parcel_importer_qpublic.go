package importers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/renderyourworld/parcel_heatmap/models"
	"github.com/renderyourworld/parcel_heatmap/utils"
	"gorm.io/gorm"
)

// The payload sent to the Beacon API
type VectorLayerRequest struct {
	LayerID         int               `json:"layerId"`
	UseSelection    bool              `json:"useSelection"`
	Ext             utils.BoundingBox `json:"ext"`
	Wkt             *string           `json:"wkt"`
	SpatialRelation int               `json:"spatialRelation"`
	FeatureLimit    int               `json:"featureLimit"`
}

// Represents a single parcel returned from the QPublic API
type QPublicParcel struct {
	Key         string `json:"Key"`
	WktGeometry string `json:"WktGeometry"`
	Address     string `json:"Address"`
	TipHtml     string `json:"TipHtml"`
	ResultHtml  string `json:"ResultHtml"`
}

// What the Beacon API returns
type VectorLayerResponse struct {
	D []QPublicParcel `json:"d"` // Contains the array of parcels
}

// Manages QPS tokens and source SRID for QPublic API
type TokenManager struct {
	Token       string
	TokenExpiry time.Time
	SourceSRID  int // The SRID of the parcel geometry data
	HTTPClient  *http.Client
	GisApiUrl   string // The county's GIS API URL (contains AppID and LayerID)
}

// Uses chromedp to fetch a new QPS token and source SRID from the page
func (tm *TokenManager) FetchNewToken() (string, int, error) {
	ctx, cleanup := NewRandomizedBrowser()
	defer cleanup()

	log.Println("Launching programmatic browser to fetch token...")

	var qps string
	var srid float64

	err := chromedp.Run(ctx,
		chromedp.Navigate(tm.GisApiUrl),
		// Wait for the scripts to execute and mapConfig to be available
		chromedp.Sleep(time.Duration(5+rand.Intn(6))*time.Second),
		// Extract only the values we need to avoid circular reference issues
		chromedp.Evaluate(`mapConfig.QPS`, &qps),
		chromedp.Evaluate(`mapConfig.SRID || 2240`, &srid),
	)

	if err != nil {
		return "", 0, fmt.Errorf("chromedp execution failed: %w", err)
	}

	if qps == "" {
		return "", 0, fmt.Errorf("QPS not found in mapConfig")
	}

	return qps, int(srid), nil
}

// Returns a cached token or fetches a new one if needed
func (tm *TokenManager) GetQPSToken(forceRefresh bool) (string, error) {
	// Check if we need a new token
	needsRefresh := forceRefresh || tm.Token == "" || time.Now().After(tm.TokenExpiry)

	if needsRefresh {
		log.Println("Fetching new QPS token...")

		// Fetch new token
		token, srid, err := tm.FetchNewToken()
		if err != nil {
			return "", fmt.Errorf("failed to fetch new token: %w", err)
		}

		// Set token and expiry
		tm.Token = token
		tm.SourceSRID = srid
		// Set expiration to 7.5hrs from now (buffer before the 8hr limit on QPS tokens)
		tm.TokenExpiry = time.Now().Add(7*time.Hour + 30*time.Minute)

		log.Printf("Token valid until: %s, Source SRID: %d", tm.TokenExpiry.Format("Jan 2 3:04 PM"), tm.SourceSRID)
	}

	return tm.Token, nil
}

// Creates a new TokenManager for QPublic API
func NewTokenManager(gisApiUrl string) *TokenManager {
	return &TokenManager{
		GisApiUrl: gisApiUrl,
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Handles scraping parcels from QPublic API
type QPublicScraper struct {
	TokenManager   *TokenManager
	RequestCount   int
	TokenRefreshes int
	MaxDepth       int                      // Maximum recursion depth reached
	UniqueParcels  map[string]QPublicParcel // Deduplicated parcels (key = parcelKey|wktGeometry)
	LayerID        int                      // Layer ID extracted from URL
}

// Fetches parcels within a bounding box
func (ps *QPublicScraper) FetchParcels(bbox utils.BoundingBox) ([]QPublicParcel, error) {
	// Get current token
	qpsToken, err := ps.TokenManager.GetQPSToken(false)
	if err != nil {
		return nil, fmt.Errorf("failed to get QPS token: %w", err)
	}

	// Build the request payload
	payload := VectorLayerRequest{
		LayerID:         ps.LayerID,
		UseSelection:    false,
		Ext:             bbox,
		Wkt:             nil,
		SpatialRelation: 1,
		FeatureLimit:    500,
	}

	// Marshal payload to json
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Build the url with the qps token
	url := fmt.Sprintf("https://qpublic.schneidercorp.com/api/beaconCore/GetVectorLayer?QPS=%s", qpsToken)

	// Create the POST request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	// Apply randomized headers
	headers := getRandomHeaders(ps.TokenManager.GisApiUrl)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Make the request
	resp, err := ps.TokenManager.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	ps.RequestCount++

	// Handle 403 (Cloudflare Challenge)
	if resp.StatusCode == 403 {
		log.Println("Got 403, waiting 3s and retrying with new headers...")
		time.Sleep(3 * time.Second)

		// Retry once with fresh headers
		req, _ = http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
		headers = getRandomHeaders(ps.TokenManager.GisApiUrl) // New random headers
		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err = ps.TokenManager.HTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("retry failed: %w", err)
		}
		defer resp.Body.Close()
		ps.RequestCount++

		// If still 403, bail out
		if resp.StatusCode == 403 {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("got 403 after retry: %s", string(body[:min(200, len(body))]))
		}
	}

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s",
			resp.StatusCode, string(body))
	}

	// Parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response VectorLayerResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return response.D, nil
}

// Recursively fetches parcels, subdividing when hitting the 500 limit
func (ps *QPublicScraper) FetchRegionRecursive(bbox utils.BoundingBox, depth int) error {
	// Update max depth tracked
	if depth > ps.MaxDepth {
		ps.MaxDepth = depth
	}

	// Prevent infinite recursion
	if depth > 10 {
		return fmt.Errorf("exceeded maximum recursion depth")
	}

	// Calculate region info
	width := bbox.MaxX - bbox.MinX
	height := bbox.MaxY - bbox.MinY
	area := (width * height) / 1_000_000 // Millions of square feet

	log.Printf("%s Depth %d: %.0f × %.0f ft (%.1fM sq ft)",
		indent(depth), depth, width, height, area)

	// Fetch parcels in the region
	parcels, err := ps.FetchParcels(bbox)
	if err != nil {
		return fmt.Errorf("failed to fetch parcels: %w", err)
	}

	log.Printf("%s✓ Got %d parcels (Request #%d)", indent(depth), len(parcels), ps.RequestCount)

	// Check if we hit the 500 limit
	if len(parcels) == 500 {
		log.Printf("%s Hit limit! Subdividing into 4 quadrants", indent(depth))

		// Calculate midpoints
		midX := (bbox.MinX + bbox.MaxX) / 2
		midY := (bbox.MinY + bbox.MaxY) / 2

		// Add small overlap to avoid missing parcels on boundaries
		overlapX := (bbox.MaxX - bbox.MinX) * 0.001
		overlapY := (bbox.MaxY - bbox.MinY) * 0.001

		// Create 4 quadrants
		quadrants := []utils.BoundingBox{
			// Bottom-left
			{MinX: bbox.MinX, MinY: bbox.MinY,
				MaxX: midX + overlapX, MaxY: midY + overlapY},
			// Bottom-right
			{MinX: midX - overlapX, MinY: bbox.MinY,
				MaxX: bbox.MaxX, MaxY: midY + overlapY},
			// Top-left
			{MinX: bbox.MinX, MinY: midY - overlapY,
				MaxX: midX + overlapX, MaxY: bbox.MaxY},
			// Top-right
			{MinX: midX - overlapX, MinY: midY - overlapY,
				MaxX: bbox.MaxX, MaxY: bbox.MaxY},
		}

		// Recursively fetch each quadrant
		for i, quad := range quadrants {
			log.Printf("%s┌─ Quadrant %d/%d", indent(depth), i+1, 4)
			err := ps.FetchRegionRecursive(quad, depth+1)
			if err != nil {
				return err
			}
			log.Printf("%s└─ Quadrant %d complete", indent(depth), i+1)
		}
	} else {
		// Got less than 500, deduplicate directly into map
		for _, parcel := range parcels {
			if parcel.Key != "" {
				// Create composite key: parcelKey|wktGeometry
				// Same key + different geometry = multi-part parcel (keep both)
				// Same key + same geometry = duplicate from overlap (keep one)
				dedupKey := parcel.Key + "|" + parcel.WktGeometry
				ps.UniqueParcels[dedupKey] = parcel
			}
		}
	}

	// Rate limiting - be nice to their servers
	jitter := time.Duration(rand.Intn(2000)) * time.Millisecond // 0-2 seconds
	time.Sleep(3*time.Second + jitter)

	return nil
}

// The main entry point for county scraping. Fetches all parcels for a county
func (ps *QPublicScraper) ScrapeCounty(bbox utils.BoundingBox) ([]QPublicParcel, error) {
	log.Println(" Starting QPublic parcel collection")
	log.Printf("   County bounds: %.0f, %.0f, %.0f, %.0f",
		bbox.MinX, bbox.MinY, bbox.MaxX, bbox.MaxY)

	startTime := time.Now()

	// Reset collection
	ps.UniqueParcels = make(map[string]QPublicParcel)

	// Start recursive collection (deduplicates into ps.UniqueParcels)
	err := ps.FetchRegionRecursive(bbox, 0)
	if err != nil {
		return nil, err
	}

	elapsed := time.Since(startTime)

	// Print summary
	log.Println("\n Collection complete!")
	log.Printf("   Requests made: %d", ps.RequestCount)
	log.Printf("   Token refreshes: %d", ps.TokenRefreshes)
	log.Printf("   Max depth reached: %d", ps.MaxDepth)
	log.Printf("   Unique parcels: %d", len(ps.UniqueParcels))
	log.Printf("   Time elapsed: %.1f min", elapsed.Minutes())
	if ps.RequestCount > 0 {
		log.Printf("   Avg per request: %.1f sec",
			elapsed.Seconds()/float64(ps.RequestCount))
	}

	// Convert map to slice
	result := make([]QPublicParcel, 0, len(ps.UniqueParcels))
	for _, parcel := range ps.UniqueParcels {
		result = append(result, parcel)
	}

	return result, nil
}

// Creates a new scraper for QPublic API
func NewQPublicScraper(tokenManager *TokenManager, layerID int) *QPublicScraper {
	return &QPublicScraper{
		TokenManager:  tokenManager,
		LayerID:       layerID,
		RequestCount:  0,
		MaxDepth:      0,
		UniqueParcels: make(map[string]QPublicParcel),
	}
}

// Extracts the LayerID parameter from a QPublic URL
func parseLayerIDFromURL(gisApiUrl string) (int, error) {
	parsedURL, err := url.Parse(gisApiUrl)
	if err != nil {
		return 0, fmt.Errorf("failed to parse URL: %w", err)
	}

	layerIDStr := parsedURL.Query().Get("LayerID")
	if layerIDStr == "" {
		return 0, fmt.Errorf("LayerID not found in URL")
	}

	layerID, err := strconv.Atoi(layerIDStr)
	if err != nil {
		return 0, fmt.Errorf("invalid LayerID value: %w", err)
	}

	return layerID, nil
}

// Returns spacing for visual tree structure
func indent(depth int) string {
	s := ""
	for i := 0; i < depth; i++ {
		s += "  "
	}
	return s
}

func getRandomHeaders(referer string) map[string]string {
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	}

	acceptLanguages := []string{
		"en-US,en;q=0.9",
		"en-GB,en;q=0.9",
		"en-US,en;q=0.9,es;q=0.8",
		"en-CA,en;q=0.9",
	}

	// Randomly include or exclude some headers
	headers := map[string]string{
		"Content-Type":    "application/json",
		"User-Agent":      userAgents[rand.Intn(len(userAgents))],
		"Accept":          "application/json, text/plain, */*",
		"Referer":         referer,
		"Accept-Language": acceptLanguages[rand.Intn(len(acceptLanguages))],
	}

	// Sometimes add these extra headers (50% chance each)
	if rand.Float32() < 0.5 {
		headers["Connection"] = "keep-alive"
	}

	if rand.Float32() < 0.5 {
		headers["Sec-Fetch-Dest"] = "empty"
		headers["Sec-Fetch-Mode"] = "cors"
		headers["Sec-Fetch-Site"] = "same-origin"
	}

	// Vary cache control
	cacheOptions := []string{"no-cache", "max-age=0", ""}
	cache := cacheOptions[rand.Intn(len(cacheOptions))]
	if cache != "" {
		headers["Cache-Control"] = cache
	}

	return headers
}

func NewRandomizedBrowser() (context.Context, context.CancelFunc) {
	userDataDir, _ := os.MkdirTemp("", "scraper-*")

	// Randomize some fingerprint elements
	windowWidth := 1024 + rand.Intn(1476) // 1024-2500
	windowHeight := 768 + rand.Intn(672)  // 768-1440

	// Rotate through different user agents
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/119.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/121.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/119.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36",
	}

	// Randomly include/exclude some flags
	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", false),
		chromedp.UserDataDir(userDataDir),
		chromedp.UserAgent(userAgents[rand.Intn(len(userAgents))]),
		chromedp.WindowSize(windowWidth, windowHeight),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-sync", true),
	}

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel2 := chromedp.NewContext(allocCtx)

	cleanup := func() {
		cancel2()
		cancel()
		os.RemoveAll(userDataDir)
	}

	return ctx, cleanup
}

// Fetches the county bounding box and transforms it to the source SRID
func getCountyBoundsForQPublic(db *gorm.DB, countyID uint16, sourceSRID int) (*utils.BoundingBox, error) {
	var minX, minY, maxX, maxY float64
	query := fmt.Sprintf(`
		SELECT ST_XMin(tb), ST_YMin(tb), ST_XMax(tb), ST_YMax(tb)
        FROM (SELECT ST_Transform(bbox, %d) as tb FROM counties WHERE id = %d) as sub
	`, sourceSRID, countyID)

	row := db.Raw(query).Row()
	if err := row.Scan(&minX, &minY, &maxX, &maxY); err != nil {
		return nil, fmt.Errorf("failed to fetch county bbox: %w", err)
	}

	return &utils.BoundingBox{
		MinX: minX,
		MinY: minY,
		MaxX: maxX,
		MaxY: maxY,
	}, nil
}

// Batch inserts QPublic parcels into the database
func insertQPublicParcels(db *gorm.DB, parcels []QPublicParcel, countyID uint16, sourceSRID int, logging bool) error {
	if len(parcels) == 0 {
		return nil
	}

	log.Printf("Inserting %d parcels into database (County ID: %d)...", len(parcels), countyID)

	// Initialize performance logger
	perfLogger := utils.NewPerfLogger(logging)
	if logging {
		log.Println("Performance logging enabled for insertions")
	}

	objectId := 1
	successItems := 0
	failedItems := 0
	skippedItems := 0
	// Parse regex for owner and acres
	ownerRegex := regexp.MustCompile(`<b>Owner</b> - (.*?)<br>`)
	acresRegex := regexp.MustCompile(`<b>Acres</b> - (.*?)<br>`)

	// Batch processing
	const batchSize = 500
	for i := 0; i < len(parcels); i += batchSize {
		end := i + batchSize
		if end > len(parcels) {
			end = len(parcels)
		}

		batch := parcels[i:end]
		var valueStrings []string
		var valueArgs []interface{}
		argCount := 1

		for _, p := range batch {
			// "Key" and "WktGeometry" need to not be empty
			if p.Key == "" || p.WktGeometry == "" {
				skippedItems++
				continue
			}

			// Parse Owner
			ownerMatch := ownerRegex.FindStringSubmatch(p.ResultHtml)
			owner := ""
			if len(ownerMatch) > 1 {
				owner = strings.TrimSpace(ownerMatch[1])
			}

			// Parse Acres
			acresMatch := acresRegex.FindStringSubmatch(p.ResultHtml)
			var acres float64
			if len(acresMatch) > 1 {
				if val, err := strconv.ParseFloat(strings.TrimSpace(acresMatch[1]), 64); err == nil {
					acres = val
				}
			}

			// Parse Site Number (first part of address if numeric)
			siteNumber := ""
			addrParts := strings.Fields(p.Address)
			if len(addrParts) > 0 {
				// Check if it looks like a number
				if _, err := strconv.Atoi(addrParts[0]); err == nil {
					siteNumber = addrParts[0]
				}
			}

			// "WktGeometry" conversion and SRID transformation:
			valueStrings = append(valueStrings, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, ST_Transform(ST_Multi(ST_GeomFromText($%d, %d)), 3857))",
				argCount, argCount+1, argCount+2, argCount+3, argCount+4, argCount+5, argCount+6, argCount+7, sourceSRID))

			valueArgs = append(valueArgs, countyID, p.Key, objectId, p.Address, siteNumber, owner, acres, p.WktGeometry)
			argCount += 8
			objectId++
		}

		if len(valueStrings) == 0 {
			continue
		}

		// Using ON CONFLICT DO NOTHING to avoid duplicate errors
		query := fmt.Sprintf(`
				INSERT INTO parcels (county_id, parcel_id, objectid, site_address, site_number, owner_name, acres, geometry) 
				VALUES %s
				ON CONFLICT DO NOTHING
			`, strings.Join(valueStrings, ","))

		if err := db.Exec(query, valueArgs...).Error; err != nil {
			log.Printf("ERROR: Failed to insert batch %d-%d for county %d: %v", i+1, end, countyID, err)
			log.Printf("FAILING QUERY: %s", interpolateSQLForLog(query, valueArgs))
			failedItems += len(valueStrings)
			continue
		}

		successItems += len(valueStrings)

		// Update performance logger
		perfLogger.Update(successItems+failedItems, 10*time.Second)

		log.Printf("Inserted batch %d-%d of %d", i+1, end, len(parcels))
	}

	// Log final performance summary
	perfLogger.LogFinal()

	log.Printf(" Insert complete: %d parcels successful, %d parcels failed, %d parcels skipped", successItems, failedItems, skippedItems)
	return nil
}

// A helper to create a copy-pasteable SQL string for debugging.
// It replaces $1, $2, etc. with actual values from args.
func interpolateSQLForLog(query string, args []interface{}) string {
	interpolated := query
	// Replace in reverse order (e.g., $10 before $1) to avoid partial matches
	for i := len(args); i > 0; i-- {
		placeholder := fmt.Sprintf("$%d", i)
		arg := args[i-1]
		var val string
		switch v := arg.(type) {
		case string:
			// Escape single quotes for SQL
			safeStr := strings.ReplaceAll(v, "'", "''")
			val = fmt.Sprintf("'%s'", safeStr)
		case int, int32, int64, uint16, uint32, float64:
			val = fmt.Sprintf("%v", v)
		case bool:
			val = fmt.Sprintf("%v", v)
		case nil:
			val = "NULL"
		default:
			val = fmt.Sprintf("'%v'", v)
		}
		interpolated = strings.ReplaceAll(interpolated, placeholder, val)
	}
	return interpolated
}

// Imports parcels from a QPublic-hosted county
func startQPublicImporter(gormDB *gorm.DB, county *models.County, logging bool) error {
	log.Printf("Starting QPublic importer for %s", county.Name)
	log.Printf("Found county: %s (ID: %d, API: %s)", county.Name, county.ID, county.GisApiUrl.String)

	// Extract LayerID from URL
	layerID, err := parseLayerIDFromURL(county.GisApiUrl.String)
	if err != nil {
		return fmt.Errorf("failed to parse LayerID from URL: %w", err)
	}
	log.Printf("Extracted LayerID: %d", layerID)

	// Create token manager
	tokenManager := NewTokenManager(county.GisApiUrl.String)

	// Pre-fetch token to get SRID before getting bounds
	_, err = tokenManager.GetQPSToken(false)
	if err != nil {
		return fmt.Errorf("failed to get initial QPS token: %w", err)
	}

	// Get county bounds in source SRID
	bbox, err := getCountyBoundsForQPublic(gormDB, county.ID, tokenManager.SourceSRID)
	if err != nil {
		return fmt.Errorf("failed to get county bounds: %w", err)
	}

	log.Printf("County BBox (SRID %d): %.0f, %.0f, %.0f, %.0f",
		tokenManager.SourceSRID, bbox.MinX, bbox.MinY, bbox.MaxX, bbox.MaxY)

	// Create scraper
	scraper := NewQPublicScraper(tokenManager, layerID)

	// Scrape the entire county
	parcels, err := scraper.ScrapeCounty(*bbox)
	if err != nil {
		return fmt.Errorf("scraping failed: %w", err)
	}

	log.Printf("Got %d parcels, inserting into database...", len(parcels))

	// Insert into DB with performance logging
	if err := insertQPublicParcels(gormDB, parcels, county.ID, tokenManager.SourceSRID, logging); err != nil {
		return fmt.Errorf("failed to insert parcels: %w", err)
	}

	log.Printf("QPublic import complete! Total parcels: %d", len(parcels))

	return nil
}
