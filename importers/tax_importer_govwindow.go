package importers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/renderyourworld/parcel_heatmap/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// User agent in linux with docker
	// userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"
	// userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"

	// Batch size for claiming parcels from the queue
	govWindowBatchSize = 10

	// Claims older than this are considered stale (worker crashed/failed)
	staleClaimTimeout = "10 minutes"
)

const fallbackUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/0.0.0.0 Safari/537.36"

// getRuntimeUserAgent asks the connected Chrome instance for its native UA.
// If the call fails, it returns a generic Linux Chrome UA as fallback.
func getRuntimeUserAgent(browser *rod.Browser) string {
	version, err := proto.BrowserGetVersion{}.Call(browser)
	if err != nil {
		log.Printf("Warning: failed to fetch browser version for UA override: %v; using fallback UA", err)
		return fallbackUserAgent
	}

	ua := version.UserAgent
	if ua == "" {
		log.Printf("Warning: browser returned empty UA; using fallback UA")
		return fallbackUserAgent
	}

	// Some environments expose "HeadlessChrome". Present it as regular Chrome.
	return strings.Replace(ua, "HeadlessChrome/", "Chrome/", 1)
}

// deriveGovWindowURLs extracts the base domain from the county's TaxApiUrl.
// Returns (rootURL, baseURL, error) where rootURL is the full TaxApiUrl
// and baseURL is just scheme://host for constructing sub-page URLs.
func deriveGovWindowURLs(county *models.County) (string, string, error) {
	if !county.TaxApiUrl.Valid || county.TaxApiUrl.String == "" {
		return "", "", fmt.Errorf("county %s has no TaxApiUrl configured", county.Name)
	}

	taxURL := county.TaxApiUrl.String
	parsed, err := url.Parse(taxURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse TaxApiUrl '%s': %w", taxURL, err)
	}

	rootURL := taxURL
	baseURL := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)

	return rootURL, baseURL, nil
}

// TaxData holds extracted tax information from a single tax bill
type TaxData struct {
	URL             string
	BillNo          string
	TaxYear         string
	TaxPayer        string
	Location        string
	District        string
	BuildingValue   string
	LandValue       string
	FairMarketValue string
	DueDate         string
	TaxAmount       string
	BackTaxes       string
	TotalDue        string
	PaidDate        string
	LastUpdated     time.Time
}

// TaxBillLink represents a link to a tax bill found on select_bill.html
type TaxBillLink struct {
	BillNo  string
	TaxYear string
	URL     string
}

// NetworkMonitor tracks bytes transferred for each page load using atomic operations
// for thread-safety since CDP events fire on a different goroutine
type NetworkMonitor struct {
	currentBytes int64 // Bytes for current page load (reset before each navigation)
	totalBytes   int64 // Cumulative bytes transferred
}

// NewNetworkMonitor creates a new network monitor
func NewNetworkMonitor() *NetworkMonitor {
	return &NetworkMonitor{}
}

// Reset resets the current request counter (call before each navigation)
func (nm *NetworkMonitor) Reset() {
	atomic.StoreInt64(&nm.currentBytes, 0)
}

// AddBytes adds bytes from a completed request (called by CDP event handler)
func (nm *NetworkMonitor) AddBytes(bytes int64) {
	atomic.AddInt64(&nm.currentBytes, bytes)
	atomic.AddInt64(&nm.totalBytes, bytes)
}

// GetCurrentBytes returns bytes for current page load
func (nm *NetworkMonitor) GetCurrentBytes() int64 {
	return atomic.LoadInt64(&nm.currentBytes)
}

// GetTotalBytes returns cumulative bytes transferred
func (nm *NetworkMonitor) GetTotalBytes() int64 {
	return atomic.LoadInt64(&nm.totalBytes)
}

// FormatBytes formats bytes as human-readable string (KB, MB, GB)
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// QueueItem represents a single parcel claimed from the queue table
type QueueItem struct {
	ID       uint64 `gorm:"column:id"`
	ParcelID string `gorm:"column:parcel_id"`
}

// queueTableName returns the dynamic table name for a county's tax queue
func queueTableName(countyID uint16) string {
	return fmt.Sprintf("tax_queue_%d", countyID)
}

// ensureQueueTable creates the queue table if it doesn't exist, resets stale
// claims from crashed workers, and populates it if empty. Returns the count
// of unclaimed items in the queue.
func ensureQueueTable(gormDB *gorm.DB, countyID uint16) (int64, error) {
	table := queueTableName(countyID)

	// Create table if not exists (idempotent, safe for concurrent workers)
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGINT PRIMARY KEY,
			parcel_id VARCHAR(50) NOT NULL,
			claimed_at TIMESTAMP
		)`, table)

	if err := gormDB.Exec(createSQL).Error; err != nil {
		return 0, fmt.Errorf("failed to create queue table %s: %w", table, err)
	}

	// Reset stale claims from crashed/failed workers
	resetSQL := fmt.Sprintf(`
		UPDATE %s SET claimed_at = NULL
		WHERE claimed_at IS NOT NULL
		  AND claimed_at < NOW() - INTERVAL '%s'`, table, staleClaimTimeout)

	result := gormDB.Exec(resetSQL)
	if result.Error != nil {
		log.Printf("Warning: failed to reset stale claims: %v", result.Error)
	} else if result.RowsAffected > 0 {
		log.Printf("Reset %d stale claims in %s", result.RowsAffected, table)
	}

	// Count unclaimed items
	var count int64
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE claimed_at IS NULL", table)
	if err := gormDB.Raw(countSQL).Scan(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count queue table %s: %w", table, err)
	}

	// If no unclaimed items, check if table is completely empty (vs all claimed)
	if count == 0 {
		var totalCount int64
		totalSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
		if err := gormDB.Raw(totalSQL).Scan(&totalCount).Error; err != nil {
			return 0, fmt.Errorf("failed to count total in %s: %w", table, err)
		}

		if totalCount > 0 {
			// Table has rows but all are claimed by active workers
			log.Printf("Queue table %s has %d parcels, all currently claimed by other workers", table, totalCount)
			return 0, nil
		}

		// Table is truly empty, populate from parcels
		log.Printf("Queue table %s is empty, populating from parcels...", table)

		populateSQL := fmt.Sprintf(`
			INSERT INTO %s (id, parcel_id)
			SELECT id, parcel_id FROM parcels
			WHERE county_id = ?
			  AND parcel_id IS NOT NULL
			  AND parcel_id != ''
			  AND parcel_id NOT ILIKE '%%COUNTY%%'
			ON CONFLICT DO NOTHING`, table)

		populateResult := gormDB.Exec(populateSQL, countyID)
		if populateResult.Error != nil {
			return 0, fmt.Errorf("failed to populate queue table %s: %w", table, populateResult.Error)
		}

		count = populateResult.RowsAffected
		log.Printf("Populated %s with %d parcels", table, count)
	} else {
		log.Printf("Queue table %s has %d unclaimed parcels", table, count)
	}

	return count, nil
}

// claimQueueBatch atomically claims a batch of unclaimed parcels by setting claimed_at.
// Uses FOR UPDATE SKIP LOCKED for safe concurrent access by multiple workers.
// Returns empty slice when no unclaimed parcels remain.
func claimQueueBatch(gormDB *gorm.DB, countyID uint16, batchSize int) ([]QueueItem, error) {
	table := queueTableName(countyID)

	claimSQL := fmt.Sprintf(`
		UPDATE %s SET claimed_at = NOW()
		WHERE id IN (
			SELECT id FROM %s
			WHERE claimed_at IS NULL
			ORDER BY id ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, parcel_id`, table, table)

	var items []QueueItem
	if err := gormDB.Raw(claimSQL, batchSize).Scan(&items).Error; err != nil {
		return nil, fmt.Errorf("failed to claim batch from %s: %w", table, err)
	}

	return items, nil
}

// completeQueueItem removes a successfully processed parcel from the queue
func completeQueueItem(gormDB *gorm.DB, countyID uint16, parcelID uint64) error {
	table := queueTableName(countyID)
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE id = ?", table)
	return gormDB.Exec(deleteSQL, parcelID).Error
}

// releaseQueueItem releases a failed parcel back to the queue for retry
func releaseQueueItem(gormDB *gorm.DB, countyID uint16, parcelID uint64) error {
	table := queueTableName(countyID)
	releaseSQL := fmt.Sprintf("UPDATE %s SET claimed_at = NULL WHERE id = ?", table)
	return gormDB.Exec(releaseSQL, parcelID).Error
}

// releaseQueueBatch releases all parcels in a batch back to the queue
func releaseQueueBatch(gormDB *gorm.DB, countyID uint16, items []QueueItem) {
	for _, item := range items {
		if err := releaseQueueItem(gormDB, countyID, item.ID); err != nil {
			log.Printf("Warning: failed to release queue item %d: %v", item.ID, err)
		}
	}
}

// heartbeatQueueBatch refreshes claimed_at on remaining batch items to prevent
// stale claim recovery from reclaiming parcels that are still being actively processed.
// Call this after each parcel completes to prove the worker is still alive.
func heartbeatQueueBatch(gormDB *gorm.DB, countyID uint16, items []QueueItem) {
	if len(items) == 0 {
		return
	}
	table := queueTableName(countyID)
	ids := make([]uint64, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	heartbeatSQL := fmt.Sprintf("UPDATE %s SET claimed_at = NOW() WHERE id IN ?", table)
	if err := gormDB.Exec(heartbeatSQL, ids).Error; err != nil {
		log.Printf("Warning: failed to heartbeat queue items: %v", err)
	}
}

// getQueueRemaining returns the number of unclaimed parcels still in the queue
func getQueueRemaining(gormDB *gorm.DB, countyID uint16) (int64, error) {
	table := queueTableName(countyID)
	var count int64
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE claimed_at IS NULL", table)
	if err := gormDB.Raw(countSQL).Scan(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count remaining in %s: %w", table, err)
	}
	return count, nil
}

// getQueueTotal returns the total number of parcels in the queue (both claimed and unclaimed)
func getQueueTotal(gormDB *gorm.DB, countyID uint16) (int64, error) {
	table := queueTableName(countyID)
	var count int64
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	if err := gormDB.Raw(countSQL).Scan(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count total in %s: %w", table, err)
	}
	return count, nil
}

// getCountyParcelCount returns the total number of parcels in the county from
// the parcels table. This is the true total used for overall progress tracking.
func getCountyParcelCount(gormDB *gorm.DB, countyID uint16) (int64, error) {
	var count int64
	err := gormDB.Raw(`
		SELECT COUNT(*) FROM parcels
		WHERE county_id = ?
		  AND parcel_id IS NOT NULL
		  AND parcel_id != ''
		  AND parcel_id NOT ILIKE '%COUNTY%'`, countyID).Scan(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count parcels for county %d: %w", countyID, err)
	}
	return count, nil
}

// sendDashboardHeartbeat sends a progress heartbeat to the dashboard server.
// This is fire-and-forget; errors are logged but don't affect scraping.
func sendDashboardHeartbeat(dashboardURL string, instance int, countyName string, totalParcels int, queueRemaining int64, completedParcels int, batchSize int, batchCompleted int, status string) {
	if dashboardURL == "" {
		return
	}

	machineID := os.Getenv("MACHINE_ID")

	payload := map[string]interface{}{
		"instance":          instance,
		"machine_id":        machineID,
		"county":            countyName,
		"status":            status,
		"total_parcels":     totalParcels,
		"queue_remaining":   int(queueRemaining),
		"completed_parcels": completedParcels,
		"batch_size":        batchSize,
		"batch_completed":   batchCompleted,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Warning: failed to marshal heartbeat: %v", err)
		return
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(dashboardURL+"/api/heartbeat", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("Warning: dashboard heartbeat failed: %v", err)
		return
	}
	resp.Body.Close()
}

// ProgressTracker tracks scraping progress
type ProgressTracker struct {
	StartTime             time.Time
	ParcelsTotal          int
	ParcelsSuccess        int
	ParcelsFailed         int
	TaxBillsScraped       int
	TotalRequests         int   // Track total page navigations for variable rhythm
	TotalBytesTransferred int64 // Cumulative bytes transferred
	LastRequestBytes      int64 // Bytes from most recent page load
	LastLogTime           time.Time
	LastParcelID          uint64
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{
		StartTime:   time.Now(),
		LastLogTime: time.Now(),
	}
}

// LogProgress logs current progress
func (p *ProgressTracker) LogProgress() {
	elapsed := time.Since(p.StartTime)
	rate := 0.0
	if elapsed.Minutes() > 0 {
		rate = float64(p.ParcelsSuccess) / elapsed.Minutes()
	}

	log.Println("")
	log.Println(strings.Repeat("=", 50))
	log.Println("=== PROGRESS ===")
	log.Println(strings.Repeat("=", 50))
	log.Printf("Elapsed:        %v", elapsed.Round(time.Second))
	log.Printf("Parcels:        %d success, %d failed", p.ParcelsSuccess, p.ParcelsFailed)
	log.Printf("Tax Bills:      %d scraped", p.TaxBillsScraped)
	log.Printf("Total Requests: %d", p.TotalRequests)
	log.Printf("Rate:           %.2f parcels/min", rate)
	log.Printf("Data Transfer:  %s total", FormatBytes(p.TotalBytesTransferred))
	if p.TotalRequests > 0 {
		avgBytes := p.TotalBytesTransferred / int64(p.TotalRequests)
		log.Printf("Avg/Request:    %s", FormatBytes(avgBytes))
	}
	log.Printf("Last Parcel ID: %d", p.LastParcelID)
	log.Println(strings.Repeat("=", 50))
	log.Println("")

	p.LastLogTime = time.Now()
}

// ShouldLog returns true if enough time has passed to log progress
func (p *ProgressTracker) ShouldLog(interval time.Duration) bool {
	return time.Since(p.LastLogTime) >= interval
}

// stripParcelID removes all whitespace from parcel ID for initial search
// The website will format it correctly when we navigate to the search URL
func stripParcelID(parcelID string) string {
	return strings.Join(strings.Fields(parcelID), "")
}

// buildInitialSearchURL builds the URL for initial parcel search with stripped parcel ID
// The website will reformat the URL with properly formatted map_code after navigation
func buildInitialSearchURL(baseURL string, parcelID string) string {
	stripped := stripParcelID(parcelID)
	return fmt.Sprintf("%s/select_bill.html?search_mode=search2&map_code=%s", baseURL, stripped)
}

// extractFormattedMapCode extracts the properly formatted map_code from the current page URL
// After navigating to the search URL, the website reformats the URL with the correct spacing
// We extract the raw value to preserve the "+" characters (url.Query().Get() decodes them to spaces)
func extractFormattedMapCode(page *rod.Page) (string, error) {
	currentURL := page.MustInfo().URL

	// Find map_code= in the URL and extract the raw value (preserving + characters)
	mapCodePrefix := "map_code="
	startIdx := strings.Index(currentURL, mapCodePrefix)
	if startIdx == -1 {
		return "", fmt.Errorf("map_code not found in URL: %s", currentURL)
	}

	startIdx += len(mapCodePrefix)
	endIdx := strings.IndexAny(currentURL[startIdx:], "&")
	if endIdx == -1 {
		// map_code is the last parameter
		return currentURL[startIdx:], nil
	}

	return currentURL[startIdx : startIdx+endIdx], nil
}

// buildSelectBillURL builds the select_bill.html URL using the properly formatted map_code
func buildSelectBillURL(baseURL string, formattedMapCode string) string {
	return fmt.Sprintf("%s/select_bill.html?search_mode=search2&map_code=%s&map_type=&logic_unique_key=%s",
		baseURL, formattedMapCode, formattedMapCode)
}

// CreateBrowser connects to an existing Chrome instance with remote debugging enabled.
func CreateBrowser(controlURL string) *rod.Browser {
	u := launcher.MustResolveURL(controlURL)
	browser := rod.New().ControlURL(u).MustConnect()
	return browser
}

// ApplyStealthScripts injects stealth JavaScript that runs before any page JS on every navigation.
// Uses Page.addScriptToEvaluateOnNewDocument (same .Call(page) pattern as proto.NetworkEnable at line 1160).
func ApplyStealthScripts(page *rod.Page) error {
	_, err := proto.PageAddScriptToEvaluateOnNewDocument{
		Source: `
			Object.defineProperty(navigator, 'webdriver', { get: () => undefined });

			window.chrome = {
				runtime: {},
				loadTimes: function() {},
				csi: function() {},
			};

			Object.defineProperty(navigator, 'plugins', {
				get: () => {
					function makePlugin(name, desc, filename, types) {
						const p = { name: name, description: desc, filename: filename, length: types.length };
						types.forEach(function(t, i) { p[i] = t; });
						return p;
					}
					return [
						makePlugin('PDF Viewer', 'Portable Document Format', 'internal-pdf-viewer', [
							{ type: 'application/pdf', suffixes: 'pdf', description: 'Portable Document Format' }
						]),
						makePlugin('Chrome PDF Viewer', 'Portable Document Format', 'internal-pdf-viewer', [
							{ type: 'application/pdf', suffixes: 'pdf', description: '' }
						]),
						makePlugin('Chromium PDF Viewer', 'Portable Document Format', 'internal-pdf-viewer', [
							{ type: 'application/pdf', suffixes: 'pdf', description: '' }
						]),
					];
				}
			});

			Object.defineProperty(navigator, 'languages', {
				get: () => ['en-US', 'en']
			});
		`,
	}.Call(page)
	if err != nil {
		return fmt.Errorf("failed to add stealth scripts: %w", err)
	}
	return nil
}

// SimulateHumanBehavior adds random mouse movements
func SimulateHumanBehavior(page *rod.Page) {
	for i := 0; i < 2; i++ {
		x := float64(200 + rand.Intn(600))
		y := float64(200 + rand.Intn(300))
		page.Mouse.MoveLinear(proto.Point{X: x, Y: y}, 5+rand.Intn(10))
		time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)
	}
}

// humanDelay waits 5-8 seconds with jitter (longer delay for main navigation)
func humanDelay() {
	delay := time.Duration(5000+rand.Intn(3000)) * time.Millisecond
	log.Printf("Waiting %v...", delay)
	time.Sleep(delay)
}

// shortDelay waits 2-4 seconds (for between steps within same parcel)
func shortDelay() {
	delay := time.Duration(2000+rand.Intn(2000)) * time.Millisecond
	time.Sleep(delay)
}

// readingBreak simulates a user taking time to read/review content (15-25 seconds)
func readingBreak() {
	delay := time.Duration(15000+rand.Intn(10000)) * time.Millisecond
	log.Printf("Taking a reading break... %v", delay)
	time.Sleep(delay)
}

// variableRhythmDelay implements variable timing - longer breaks every N requests
// This makes the browsing pattern more human-like
func variableRhythmDelay(requestCount int) {
	// Every 5 requests, take a longer "reading" break
	if requestCount > 0 && requestCount%5 == 0 {
		readingBreak()
	} else {
		humanDelay()
	}
}

// checkSessionValid returns true if the page doesn't show the Turnstile challenge
func checkSessionValid(html string) bool {
	return !strings.Contains(html, "Verifying that you are not a robot") &&
		!strings.Contains(html, "Just a moment")
}

// waitForCookieOrManualSolve waits for the cf_clearance cookie to appear
func waitForCookieOrManualSolve(page *rod.Page, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	log.Println("Waiting for Turnstile to be solved...")

	for time.Now().Before(deadline) {
		// Check if we're past Turnstile
		html, err := page.HTML()
		if err == nil && checkSessionValid(html) {
			// Check for cf_clearance cookie
			cookies, err := page.Cookies(nil)
			if err == nil {
				for _, cookie := range cookies {
					if cookie.Name == "cf_clearance" {
						log.Printf("cf_clearance cookie detected! (length: %d)", len(cookie.Value))
						return nil
					}
				}
			}
			// Even if cookie check fails, if page is valid we're good
			log.Println("Session appears valid (no Turnstile detected)")
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timeout waiting for cookie/session")
}

// promptForReauth prompts the user to solve Turnstile again
func promptForReauth(page *rod.Page) error {
	log.Println("")
	log.Println(strings.Repeat("!", 60))
	log.Println("SESSION EXPIRED - MANUAL ACTION REQUIRED")
	log.Println(strings.Repeat("!", 60))
	log.Println("")
	log.Println("Please solve the Cloudflare Turnstile in the browser window.")
	log.Println("Press ENTER after solving...")
	log.Println("")

	reader := bufio.NewReader(os.Stdin)
	reader.ReadString('\n')

	return waitForCookieOrManualSolve(page, 120*time.Second)
}

// parseTaxBillLinksFromPage extracts tax bill links by parsing table rows
// Table structure:
//
//	<td class="tax_year">2025</td>
//	<td class="tax_bill_no"><a href="/pay_bill.html?bill_id=XXX">33857</a></td>
func parseTaxBillLinksFromPage(page *rod.Page, baseURL string) ([]TaxBillLink, error) {
	var bills []TaxBillLink

	// Find all rows in the tax results table
	rows, err := page.Elements("#tbl_tax_results tbody tr")
	if err != nil {
		return nil, fmt.Errorf("failed to find table rows: %w", err)
	}

	log.Printf("  Parsing %d table rows...", len(rows))

	for i, row := range rows {
		// Get tax year from first cell
		yearCell, err := row.Element("td.tax_year")
		if err != nil {
			continue
		}
		taxYear, err := yearCell.Text()
		if err != nil {
			continue
		}
		taxYear = strings.TrimSpace(taxYear)

		// Filter: only tax years 2015 or newer
		yearInt, err := strconv.Atoi(taxYear)
		if err != nil || yearInt < 2015 {
			continue
		}

		// Filter: only "Real" tax type
		taxTypeCell, err := row.Element("td.tax_map_type")
		if err != nil {
			continue
		}
		taxType, err := taxTypeCell.Text()
		if err != nil {
			continue
		}
		if strings.TrimSpace(taxType) != "Real" {
			continue
		}

		// Get the link from the bill # cell
		billLink, err := row.Element("td.tax_bill_no a")
		if err != nil {
			continue
		}

		// Get bill number from link text
		billNo, err := billLink.Text()
		if err != nil {
			continue
		}
		billNo = strings.TrimSpace(billNo)

		// Get the href (contains bill_id parameter)
		href, err := billLink.Attribute("href")
		if err != nil || href == nil {
			continue
		}

		// Build full URL
		fullURL := *href
		if !strings.HasPrefix(*href, "http") {
			fullURL = baseURL + *href
		}

		log.Printf("    Row %d: year=%s, bill=%s, url=%s", i, taxYear, billNo, fullURL)

		bills = append(bills, TaxBillLink{
			BillNo:  billNo,
			TaxYear: taxYear,
			URL:     fullURL,
		})
	}

	return bills, nil
}

// ParseTaxData extracts tax information from a pay_bill.html page
func ParseTaxData(html string) (*TaxData, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	data := &TaxData{}

	// Extract from tbl_temp table
	doc.Find("#tbl_temp td").Each(func(i int, s *goquery.Selection) {
		label, exists := s.Attr("data-label")
		if !exists {
			return
		}

		value := strings.TrimSpace(s.Text())

		switch label {
		case "Tax Payer:":
			data.TaxPayer = value
		case "Location:":
			data.Location = value
		case "District:":
			data.District = value
		}
	})

	// Extract from tbl_tax_bill_item table
	doc.Find("#tbl_tax_bill_item td").Each(func(i int, s *goquery.Selection) {
		label, exists := s.Attr("data-label")
		if !exists {
			return
		}

		value := strings.TrimSpace(s.Text())

		switch label {
		case "Building Value:":
			if s.HasClass("tax_item_building_value") {
				data.BuildingValue = value
			}
		case "Land Value:":
			if s.HasClass("tax_item_land_value") {
				data.LandValue = value
			}
		case "Fair Market Value:":
			if s.HasClass("tax_item_f_m_value") {
				data.FairMarketValue = value
			}
		case "Due Date:":
			if s.HasClass("tax_item_due_date") {
				data.DueDate = value
			}
		}
	})

	// Extract from tbl_tax_detail_sumary table
	doc.Find(".tbl_tax_detail_sumary tr").Each(func(i int, s *goquery.Selection) {
		tds := s.Find("td")
		if tds.Length() == 0 {
			return
		}

		firstTd := tds.First()
		// Some rows use <strong> for the label, others use plain td text
		label := strings.TrimSpace(firstTd.Find("strong").Text())
		if label == "" {
			label = strings.TrimSpace(firstTd.Text())
		}

		value := ""
		if tds.Length() >= 2 {
			value = strings.TrimSpace(tds.Eq(1).Text())
		}

		switch label {
		case "Current Due":
			data.TaxAmount = value
		case "Back Taxes":
			data.BackTaxes = value
		case "Total Due":
			data.TotalDue = value
		case "Paid Date":
			data.PaidDate = value
		}
	})

	data.LastUpdated = time.Now()

	return data, nil
}

// parseMoneyValue converts "$1,234.56" to 1234.56
func parseMoneyValue(s string) float64 {
	// Remove $ and commas
	cleaned := strings.ReplaceAll(s, "$", "")
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.TrimSpace(cleaned)

	// Handle parentheses for negative values
	if strings.HasPrefix(cleaned, "(") && strings.HasSuffix(cleaned, ")") {
		cleaned = "-" + cleaned[1:len(cleaned)-1]
	}

	val, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0
	}
	return val
}

// saveTaxData saves the extracted tax data to the database
func saveTaxData(gormDB *gorm.DB, parcelDBID uint64, countyID uint16, taxData *TaxData) error {
	// Parse tax year
	taxYear, err := strconv.Atoi(taxData.TaxYear)
	if err != nil {
		return fmt.Errorf("invalid tax year: %s", taxData.TaxYear)
	}

	// Parse monetary values
	taxAmount := parseMoneyValue(taxData.TaxAmount)
	fmv := parseMoneyValue(taxData.FairMarketValue)
	buildingValue := parseMoneyValue(taxData.BuildingValue)
	landValue := parseMoneyValue(taxData.LandValue)
	totalDue := parseMoneyValue(taxData.TotalDue)
	backTaxes := parseMoneyValue(taxData.BackTaxes)

	// Assessed = 40% of Fair Market Value
	assessed := fmv * 0.40

	// Millage = (taxAmount / assessed) * 1000
	var millage float64
	if assessed > 0 {
		millage = (taxAmount / assessed) * 1000
	}

	lastUpdated := taxData.LastUpdated

	record := models.ParcelTax{
		CountyID:        countyID,
		ParcelID:        parcelDBID,
		TaxYear:         uint16(taxYear),
		TaxAmount:       &taxAmount,
		Appraised:       &fmv,
		Assessed:        &assessed,
		Millage:         &millage,
		PayerName:       taxData.TaxPayer,
		BillURL:         taxData.URL,
		BuildingValue:   &buildingValue,
		LandValue:       &landValue,
		DueDate:         taxData.DueDate,
		PaidDate:        taxData.PaidDate,
		TotalDue:        &totalDue,
		BackTaxes:       &backTaxes,
		LastUpdatedDate: &lastUpdated,
	}

	// Upsert: insert or update if (parcel_id, tax_year) exists
	return gormDB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "parcel_id"}, {Name: "tax_year"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"tax_amount", "appraised", "assessed", "millage", "payer_name",
			"bill_url", "building_value", "land_value",
			"due_date", "paid_date", "total_due", "back_taxes",
			"last_updated_date",
		}),
	}).Create(&record).Error
}

// scrapeTaxBill scrapes a single tax bill page and returns the data
func scrapeTaxBill(page *rod.Page, billLink TaxBillLink, progress *ProgressTracker, netMonitor *NetworkMonitor) (*TaxData, error) {
	// Reset network monitor before navigation to track this request's bytes
	netMonitor.Reset()

	// Increment request counter for this navigation
	progress.TotalRequests++
	log.Printf("  Scraping bill_no=%s, tax_year=%s (request #%d)", billLink.BillNo, billLink.TaxYear, progress.TotalRequests)

	err := page.Navigate(billLink.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to navigate: %w", err)
	}

	err = page.WaitLoad()
	if err != nil {
		return nil, fmt.Errorf("failed to wait for load: %w", err)
	}

	// Small delay for content to render and network events to complete
	time.Sleep(500 * time.Millisecond)

	// Capture bytes transferred for this page load
	progress.LastRequestBytes = netMonitor.GetCurrentBytes()
	progress.TotalBytesTransferred += progress.LastRequestBytes

	html, err := page.HTML()
	if err != nil {
		return nil, fmt.Errorf("failed to get HTML: %w", err)
	}

	// Check for session expiration
	if !checkSessionValid(html) {
		return nil, fmt.Errorf("session expired")
	}

	taxData, err := ParseTaxData(html)
	if err != nil {
		return nil, err
	}

	taxData.URL = billLink.URL
	taxData.BillNo = billLink.BillNo
	taxData.TaxYear = billLink.TaxYear

	return taxData, nil
}

// scrapeParcel scrapes all tax bills for a single parcel
func scrapeParcel(page *rod.Page, gormDB *gorm.DB, parcel models.Parcel, progress *ProgressTracker, netMonitor *NetworkMonitor, rootURL string, baseURL string, countyID uint16) error {
	// Reset network monitor and increment request counter
	netMonitor.Reset()
	progress.TotalRequests++

	// Step 1: Build initial search URL with stripped parcel ID
	initialURL := buildInitialSearchURL(baseURL, parcel.ParcelID)
	log.Printf("Scraping parcel %d: %s (request #%d)", parcel.ID, parcel.ParcelID, progress.TotalRequests)
	log.Printf("  Initial URL: %s", initialURL)

	// Navigate to search URL (website will reformat the map_code in the URL)
	err := page.Navigate(initialURL)
	if err != nil {
		return fmt.Errorf("failed to navigate to initial search URL: %w", err)
	}

	err = page.WaitLoad()
	if err != nil {
		return fmt.Errorf("failed to wait for page load: %w", err)
	}

	// Delay after page load to appear more human-like
	shortDelay()
	SimulateHumanBehavior(page)

	// Step 2: Extract the properly formatted map_code from the URL
	formattedMapCode, err := extractFormattedMapCode(page)
	if err != nil {
		return fmt.Errorf("failed to extract formatted map_code: %w", err)
	}
	log.Printf("  Formatted map_code: %s", formattedMapCode)

	// Check for session expiration on the search results page
	html, err := page.HTML()
	if err != nil {
		return fmt.Errorf("failed to get search page HTML: %w", err)
	}
	if !checkSessionValid(html) {
		return fmt.Errorf("session expired")
	}

	// Step 3: Navigate to select_bill.html with the properly formatted map_code
	// Add delay before second navigation to appear more human-like
	shortDelay()

	// Increment request counter for this navigation
	progress.TotalRequests++

	selectBillURL := buildSelectBillURL(baseURL, formattedMapCode)
	log.Printf("  Select bill URL: %s (request #%d)", selectBillURL, progress.TotalRequests)

	err = page.Navigate(selectBillURL)
	if err != nil {
		return fmt.Errorf("failed to navigate to select_bill page: %w", err)
	}

	err = page.WaitLoad()
	if err != nil {
		return fmt.Errorf("failed to wait for select_bill page load: %w", err)
	}

	// Wait for the tax results table to appear (up to 10 seconds)
	log.Printf("  Waiting for tax results table...")
	tableEl, err := page.Timeout(10 * time.Second).Element("#tbl_tax_results")
	if err != nil {
		log.Printf("  Warning: tbl_tax_results table not found, continuing anyway...")
	} else {
		// Count rows in the table
		rows, _ := tableEl.Elements("tr")
		log.Printf("  Table found with %d rows", len(rows))

		// Also count pay_bill links in the table
		links, _ := tableEl.Elements("a[href*='pay_bill.html']")
		log.Printf("  Found %d pay_bill.html links in table", len(links))
	}

	html, err = page.HTML()
	if err != nil {
		return fmt.Errorf("failed to get select_bill page HTML: %w", err)
	}

	// Check for session expiration
	if !checkSessionValid(html) {
		return fmt.Errorf("session expired")
	}

	// Step 4: Parse tax bill links from the results page using Rod directly
	billLinks, err := parseTaxBillLinksFromPage(page, baseURL)
	if err != nil {
		return fmt.Errorf("failed to parse tax bill links: %w", err)
	}

	if len(billLinks) == 0 {
		log.Printf("  No tax bills found for parcel %s", parcel.ParcelID)
		return nil
	}

	log.Printf("  Found %d tax bill(s)", len(billLinks))

	// Scrape each tax bill
	for i, billLink := range billLinks {
		// Variable rhythm delay between bills (except first one)
		// Every 5 requests, take a longer "reading" break (15-25s)
		// Otherwise, normal delay (5-8s)
		if i > 0 {
			variableRhythmDelay(progress.TotalRequests)
		}

		taxData, err := scrapeTaxBill(page, billLink, progress, netMonitor)
		if err != nil {
			if strings.Contains(err.Error(), "session expired") {
				return err // Bubble up session expiration
			}
			log.Printf("  ERROR scraping bill %s: %v", billLink.BillNo, err)
			continue
		}

		// Save to database
		if err := saveTaxData(gormDB, parcel.ID, countyID, taxData); err != nil {
			log.Printf("  ERROR saving tax data: %v", err)
			continue
		}

		progress.TaxBillsScraped++
		log.Printf("  Saved: year=%s, due=%s, fmv=%s (request #%d, %s transferred)",
			taxData.TaxYear, taxData.TaxAmount, taxData.FairMarketValue,
			progress.TotalRequests, FormatBytes(progress.LastRequestBytes))
	}

	// Navigate back to home page before next parcel (more human-like)
	log.Printf("  Navigating back to home page...")
	shortDelay()
	if err := page.Navigate(rootURL); err != nil {
		log.Printf("  Warning: failed to navigate to home page: %v", err)
	} else {
		page.WaitLoad()
		shortDelay()
	}

	return nil
}

func startGovWindowTaxImporter(gormDB *gorm.DB, county *models.County, resume bool, maxParcels int, logging bool) error {
	// Derive URLs from county's TaxApiUrl
	rootURL, baseURL, err := deriveGovWindowURLs(county)
	if err != nil {
		return err
	}
	log.Printf("GovWindow tax importer for %s (ID: %d)", county.Name, county.ID)
	log.Printf("  Root URL: %s", rootURL)
	log.Printf("  Base URL: %s", baseURL)

	// Initialize checkpoint - gates re-runs and tracks completion
	_, err = initializeCheckpoint(gormDB, county.Name, "tax", resume)
	if err != nil {
		return err
	}

	// Ensure queue table exists and is populated
	totalQueued, err := ensureQueueTable(gormDB, county.ID)
	if err != nil {
		return fmt.Errorf("failed to initialize queue: %w", err)
	}

	if totalQueued == 0 {
		log.Println("No parcels to process in queue. Import complete.")
		return nil
	}

	// Dashboard heartbeat setup
	dashboardURL := os.Getenv("DASHBOARD_URL")
	instanceNum := 0
	if v := os.Getenv("INSTANCE_NUM"); v != "" {
		instanceNum, _ = strconv.Atoi(v)
	}

	// Get true county total from parcels table for progress tracking
	countyTotal, err := getCountyParcelCount(gormDB, county.ID)
	if err != nil {
		log.Printf("Warning: could not get county parcel count: %v", err)
		countyTotal = 0
	} else {
		log.Printf("County total parcels: %d", countyTotal)
	}

	// Send initial heartbeat
	queueTotal, _ := getQueueTotal(gormDB, county.ID)
	sendDashboardHeartbeat(dashboardURL, instanceNum, county.Name, int(countyTotal), queueTotal, 0, 0, 0, "scraping")

	// Create browser connection
	log.Println("Connecting to Chrome...")

	chromeHost := os.Getenv("CHROME_HOST")
	if chromeHost == "" {
		chromeHost = "127.0.0.1"
	}

	chromePort := os.Getenv("CHROME_REMOTE_DEBUGGING_PORT")
	if chromePort == "" {
		chromePort = "9222"
	}

	debugURL := fmt.Sprintf("http://%s:%s", chromeHost, chromePort)
	log.Printf("Connecting to Chrome at %s", debugURL)

	browser := CreateBrowser(debugURL)
	defer browser.MustClose()

	page := browser.MustPage()
	defer page.MustClose()

	// Set viewport to a reasonable size (1920x1080)
	page.MustSetViewport(1920, 1080, 1, false)

	// Set user agent based on the connected Chrome runtime version.
	runtimeUA := getRuntimeUserAgent(browser)
	page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent:      runtimeUA,
		AcceptLanguage: "en-US,en;q=0.9",
		// Platform:       "Win32",
		Platform: "Linux",
	})

	// Apply stealth scripts
	ApplyStealthScripts(page)

	// Create network monitor and setup CDP event listener
	netMonitor := NewNetworkMonitor()

	// Enable network domain and listen for loading finished events
	// This captures the actual bytes transferred (EncodedDataLength)
	go page.EachEvent(func(e *proto.NetworkLoadingFinished) {
		netMonitor.AddBytes(int64(e.EncodedDataLength))
	})()

	// Enable network domain for events to fire
	proto.NetworkEnable{}.Call(page)
	log.Println("Network monitoring enabled (using browser cache for efficiency)")

	// Navigate to root URL
	log.Printf("Navigating to: %s", rootURL)
	page.Navigate(rootURL)
	page.WaitLoad()

	// Wait for initial Turnstile solve
	log.Println("")
	log.Println(strings.Repeat("=", 60))
	log.Println("INITIAL SETUP - Please solve Turnstile if prompted")
	log.Println(strings.Repeat("=", 60))
	log.Println("")

	// Check if page already has Turnstile challenge and notify dashboard
	if html, err := page.HTML(); err == nil && !checkSessionValid(html) {
		sendDashboardHeartbeat(dashboardURL, instanceNum, county.Name, int(countyTotal), queueTotal, 0, 0, 0, "captcha")
	}

	if err := waitForCookieOrManualSolve(page, 300*time.Second); err != nil {
		log.Fatalf("Failed to get initial session: %v", err)
	}

	// Clear captcha status now that session is established
	sendDashboardHeartbeat(dashboardURL, instanceNum, county.Name, int(countyTotal), queueTotal, 0, 0, 0, "scraping")
	log.Println("Session established! Starting scrape...")
	log.Println("")

	// Initialize progress tracker
	progress := NewProgressTracker()
	progress.ParcelsTotal = int(totalQueued)

	// Main scraping loop - claim batches from queue
	// Track remaining items in current batch for cleanup on early exit
	var remainingBatch []QueueItem

	for {
		items, err := claimQueueBatch(gormDB, county.ID, govWindowBatchSize)
		if err != nil {
			log.Printf("ERROR claiming batch from queue: %v", err)
			break
		}

		if len(items) == 0 {
			log.Println("Queue exhausted! No more parcels to process.")
			break
		}

		log.Printf("Claimed batch of %d parcels from queue", len(items))

		for i, item := range items {
			// Check max parcels limit
			if maxParcels > 0 && progress.ParcelsSuccess >= maxParcels {
				log.Printf("Reached max parcels limit (%d)", maxParcels)
				// Release unprocessed items from this batch back to queue
				remainingBatch = items[i:]
				goto done
			}

			// Human delay between parcels
			if progress.ParcelsSuccess > 0 || progress.ParcelsFailed > 0 {
				humanDelay()
			}

			// Convert QueueItem to models.Parcel for scrapeParcel
			parcel := models.Parcel{
				ID:       item.ID,
				ParcelID: item.ParcelID,
			}

			err := scrapeParcel(page, gormDB, parcel, progress, netMonitor, rootURL, baseURL, county.ID)
			if err != nil {
				if strings.Contains(err.Error(), "session expired") {
					log.Println("Session expired, requesting re-auth...")
					// Notify dashboard that we're waiting on a captcha
					queueNow, _ := getQueueTotal(gormDB, county.ID)
					sendDashboardHeartbeat(dashboardURL, instanceNum, county.Name, int(countyTotal), queueNow, progress.ParcelsSuccess, len(items), i, "captcha")
					if err := promptForReauth(page); err != nil {
						log.Printf("Re-auth failed: %v", err)
						log.Println("Stopping scraper to avoid further issues.")
						// Release this item and all remaining items back to queue
						remainingBatch = items[i:]
						goto done
					}
					// Captcha solved, clear captcha status
					sendDashboardHeartbeat(dashboardURL, instanceNum, county.Name, int(countyTotal), queueNow, progress.ParcelsSuccess, len(items), i, "scraping")
					// Retry this parcel after re-auth
					err = scrapeParcel(page, gormDB, parcel, progress, netMonitor, rootURL, baseURL, county.ID)
				}

				if err != nil {
					log.Printf("ERROR processing parcel %d: %v", parcel.ID, err)
					progress.ParcelsFailed++
					// Release failed parcel back to queue for retry
					releaseQueueItem(gormDB, county.ID, item.ID)
				} else {
					progress.ParcelsSuccess++
					completeQueueItem(gormDB, county.ID, item.ID)
				}
			} else {
				progress.ParcelsSuccess++
				completeQueueItem(gormDB, county.ID, item.ID)
			}

			progress.LastParcelID = parcel.ID

			// Heartbeat remaining items in this batch to prevent stale claim recovery
			if i < len(items)-1 {
				heartbeatQueueBatch(gormDB, county.ID, items[i+1:])
			}

			// Send dashboard heartbeat after every parcel for real-time batch progress
			queueNow, _ := getQueueTotal(gormDB, county.ID)
			sendDashboardHeartbeat(dashboardURL, instanceNum, county.Name, int(countyTotal), queueNow, progress.ParcelsSuccess, len(items), i+1, "scraping")

			// Log progress periodically (stdout only)
			if progress.ShouldLog(30 * time.Second) {
				remaining, _ := getQueueRemaining(gormDB, county.ID)
				log.Printf("Queue remaining: %d unclaimed parcels", remaining)
				progress.LogProgress()
			}
		}
	}

done:
	// Release any unprocessed items from the current batch back to the queue
	if len(remainingBatch) > 0 {
		log.Printf("Releasing %d unprocessed parcels back to queue...", len(remainingBatch))
		releaseQueueBatch(gormDB, county.ID, remainingBatch)
	}

	// Final progress report
	log.Println("")
	log.Println(strings.Repeat("=", 60))
	log.Println("=== SCRAPING COMPLETE ===")
	log.Println(strings.Repeat("=", 60))
	remaining, _ := getQueueRemaining(gormDB, county.ID)
	log.Printf("Queue remaining: %d unclaimed parcels", remaining)
	progress.LogProgress()

	// Send final dashboard heartbeat
	finalQueueTotal, _ := getQueueTotal(gormDB, county.ID)
	sendDashboardHeartbeat(dashboardURL, instanceNum, county.Name, int(countyTotal), finalQueueTotal, progress.ParcelsSuccess, 0, 0, "scraping")

	// If queue table is fully empty (all parcels completed by all workers),
	// mark the checkpoint as COMPLETE to prevent accidental re-runs
	totalLeft := finalQueueTotal
	if totalLeft == 0 {
		log.Println("All parcels processed! Marking checkpoint as COMPLETE.")
		completeCheckpoint(gormDB, county.Name, "tax", progress.ParcelsSuccess, progress.ParcelsFailed)
	} else {
		log.Printf("Queue still has %d parcels (other workers may still be processing)", totalLeft)
	}

	return nil
}
