package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime"
	"os"
	"strings"
	"sync"
	atomic "sync/atomic"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/renderyourworld/parcel_heatmap/benchmarks"
	"github.com/renderyourworld/parcel_heatmap/db"
	"github.com/renderyourworld/parcel_heatmap/handlers"
	"github.com/renderyourworld/parcel_heatmap/importers"
	"github.com/renderyourworld/parcel_heatmap/models"
	"github.com/renderyourworld/parcel_heatmap/tiles"
	"github.com/renderyourworld/parcel_heatmap/utils"
)

func main() {
	mime.AddExtensionType(".pmtiles", "application/vnd.pmtiles")

	// Parse command-line flags
	importParcels := flag.Bool("import-parcels", false, "Run parcel importer for specified county")
	enrichParcels := flag.Bool("enrich-parcels", false, "Run parcel enrichment for specified county")
	buildParcelSearch := flag.Bool("build-parcel-search", false, "Build parcel_search projection table for address autocomplete")
	buildOwnerGroups := flag.Bool("build-owner-groups", false, "Build materialized owner group relationships for fast reverse-owner lookup")
	importTaxes := flag.Bool("import-taxes", false, "Run parcel tax importer for specified county")
	importCounties := flag.Bool("import-counties", false, "Import all Georgia county boundaries from SAGIS API")
	county := flag.String("county", "", "County name to import parcels for")
	resume := flag.Bool("resume", false, "Resume import from last checkpoint")
	maxParcels := flag.Int("max-parcels", 0, "Maximum number of parcels to import (0 = no limit, for testing)")
	skipTiles := flag.Bool("skip-tiles", false, "Skip tile generation after import (for testing)")

	generateTiles := flag.Bool("generate-tiles", false, "Generate vector tiles for existing parcels")
	generateCountyTiles := flag.Bool("generate-county-tiles", false, "Generate vector tiles for county boundaries")
	generateTaxHeatmapTiles := flag.Bool("generate-tax-heatmap-tiles", false, "Generate precomputed tax heatmap tiles (tax_amount)")
	taxYear := flag.Int("tax-year", 0, "Tax year for heatmap tile generation (0 = all years 2015-2024)")
	minZoom := flag.Int("min-zoom", 13, "Minimum zoom level for tile generation")
	maxZoom := flag.Int("max-zoom", 19, "Maximum zoom level for tile generation")

	benchmark := flag.Bool("benchmark", false, "Run performance benchmark test using chromedp")
	benchmarkLighthouse := flag.Bool("benchmark-lighthouse", false, "Run Lighthouse audit")
	benchmarkURL := flag.String("benchmark-url", "lan", "URL to benchmark")
	benchmarkMode := flag.String("benchmark-mode", "desktop", "Benchmark mode: mobile or desktop")

	logging := flag.Bool("log", false, "Enable logging to file in logs/ directory")
	server := flag.Bool("server", false, "Run in web server mode with high connection pool")

	flag.Parse()

	// Load environment variables from .env file (if it exists)
	_ = godotenv.Load()

	// Initialize the database connection
	if *server {
		db.ConnectServer()
	} else {
		db.ConnectWorker()
	}

	// Check if we should run performance benchmark
	if *benchmark {
		now := time.Now()
		// Set up dual logging to terminal and .log file EARLY to capture all output
		cleanup, err := benchmarks.SetupBenchmarkLogger(now, *benchmarkURL)
		if err != nil {
			log.Printf("Warning: Failed to set up benchmark logging: %v", err)
		} else {
			defer cleanup()
		}

		log.Println("Starting performance benchmark...")

		// Run the benchmark
		report, err := benchmarks.RunPerformanceBenchmark(*benchmarkURL)
		if err != nil {
			log.Fatalf("Benchmark failed: %v", err)
		}

		// Print summary (will go to both terminal and file)
		benchmarks.PrintBenchmarkSummary(report)

		os.Exit(0)
	}

	if *benchmarkLighthouse {
		log.Println("Starting Lighthouse performance benchmark...")

		// Run the benchmark
		err := benchmarks.RunLighthouseAudit(*benchmarkURL, *benchmarkMode)
		if err != nil {
			log.Fatalf("Benchmark failed: %v", err)
		}

		os.Exit(0)
	}

	// Check if we should generate tiles for existing parcels
	if *generateTiles {
		if *county == "" {
			log.Fatal("Error: --county flag is required when using --generate-tiles")
		}

		var targetCounties []models.County
		if *county == "all" {
			if err := db.DB.Order("name ASC").Find(&targetCounties).Error; err != nil {
				log.Fatalf("ERROR: Failed to fetch all counties: %v", err)
			}
			log.Printf("Generating tiles for ALL %d counties...", len(targetCounties))
		} else {
			var countyRecord models.County
			if err := db.DB.Where("name = ?", *county).First(&countyRecord).Error; err != nil {
				log.Fatalf("ERROR: County '%s' not found: %v", *county, err)
			}
			targetCounties = append(targetCounties, countyRecord)
		}

		for _, c := range targetCounties {
			// Set up file logging if enabled
			var cleanup func()
			if *logging {
				var err error
				cleanup, err = utils.SetupFileLogger(c.Name, "tiles")
				if err != nil {
					log.Printf("Warning: Failed to set up file logging for %s: %v", c.Name, err)
				}
			}

			log.Printf("Generating tiles for %s county (zoom %d-%d)...", c.Name, *minZoom, *maxZoom)

			// Generate tiles
			if err := tiles.GenerateTilesForCounty(db.DB, c.ID, c.Name, *minZoom, *maxZoom, *logging); err != nil {
				log.Printf("ERROR: Tile generation failed for %s: %v", c.Name, err)
				// If generating for all, continue to next county. If for one, exit.
				if *county != "all" {
					os.Exit(1)
				}
			}

			if cleanup != nil {
				cleanup()
			}
		}

		log.Println("Tile generation completed successfully!")
		os.Exit(0)
	}

	// Check if we should generate county boundary tiles
	if *generateCountyTiles {
		// Set up file logging if enabled
		if *logging {
			cleanup, err := utils.SetupFileLogger("counties", "tiles")
			if err != nil {
				log.Printf("Warning: Failed to set up file logging: %v", err)
			} else {
				defer cleanup()
			}
		}

		log.Printf("Generating county boundary tiles (zoom %d-%d)...", *minZoom, *maxZoom)

		if err := tiles.GenerateCountyTiles(db.DB, *minZoom, *maxZoom, *logging); err != nil {
			log.Fatalf("ERROR: County tile generation failed: %v", err)
		}

		log.Println("County tile generation completed successfully!")
		os.Exit(0)
	}

	// Check if we should generate tax heatmap tiles
	if *generateTaxHeatmapTiles {
		if *county == "" {
			log.Fatal("Error: --county flag is required when using --generate-tax-heatmap-tiles")
		}

		var countyRecord models.County
		if err := db.DB.Where("name = ?", *county).First(&countyRecord).Error; err != nil {
			log.Fatalf("ERROR: County '%s' not found: %v", *county, err)
		}

		var years []int
		if *taxYear > 0 {
			years = []int{*taxYear}
		} else {
			for y := 2015; y <= 2024; y++ {
				years = append(years, y)
			}
		}

		// For heatmap v1 we use zoom 6-16 unless overridden.
		hmMin := *minZoom
		hmMax := *maxZoom
		if hmMin == 13 && hmMax == 19 {
			hmMin = 6
			hmMax = 16
		}

		log.Printf("Generating tax heatmap grid tiles for %s county years=%v zoom=%d-%d", countyRecord.Name, years, hmMin, hmMax)
		if err := tiles.GenerateTaxHeatmapTilesForCounty(db.DB, countyRecord.ID, countyRecord.Name, years, hmMin, hmMax, *logging); err != nil {
			log.Fatalf("ERROR: Tax heatmap tile generation failed: %v", err)
		}

		parcelMinZoom := 13
		parcelMaxZoom := 19
		log.Printf("Generating tax parcel tiles for %s county years=%v zoom=%d-%d", countyRecord.Name, years, parcelMinZoom, parcelMaxZoom)
		if err := tiles.GenerateTaxParcelTilesForCounty(db.DB, countyRecord.ID, countyRecord.Name, years, parcelMinZoom, parcelMaxZoom, *logging); err != nil {
			log.Fatalf("ERROR: Tax parcel tile generation failed: %v", err)
		}

		log.Println("Tax heatmap tile generation completed successfully!")
		os.Exit(0)
	}

	// Check if we should run the importer instead of starting the server
	if *importParcels {
		if *county == "" {
			log.Fatal("Error: --county flag is required when using --import-parcels")
		}

		// Split counties by comma
		counties := strings.Split(*county, ",")
		for i, c := range counties {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}

			// Set up file logging if enabled
			var cleanup func()
			if *logging {
				var err error
				cleanup, err = utils.SetupFileLogger(c, "parcels")
				if err != nil {
					log.Printf("Warning: Failed to set up file logging: %v", err)
				}
			}

			if *maxParcels > 0 {
				log.Printf("Starting parcel import for %s county (resume=%v, max=%d, skip-tiles=%v)", c, *resume, *maxParcels, *skipTiles)
			} else {
				log.Printf("Starting parcel import for %s county (resume=%v, skip-tiles=%v)", c, *resume, *skipTiles)
			}

			// Check for blocking errors (403 Forbidden / Cloudflare)
			if err := importers.StartParcelImporter(db.DB, c, *resume, *maxParcels, *logging); err != nil {
				log.Printf("ERROR: Parcel import failed for %s: %v", c, err)

				errStr := strings.ToLower(err.Error())
				if strings.Contains(errStr, "403") || strings.Contains(errStr, "forbidden") {
					log.Println("🛑 BLOCKING DETECTED (VPN/Firewall challenge). Stopping import process.")
					if cleanup != nil {
						cleanup()
					}
					os.Exit(1)
				}
			} else {
				log.Printf("Parcel import completed successfully for %s!", c)
			}

			// Generate tiles after successful import (unless skipped)
			if !*skipTiles {
				log.Printf("Generating tiles for %s county (zoom %d-%d)...", c, *minZoom, *maxZoom)

				// Look up county ID
				var countyRecord models.County
				if err := db.DB.Where("name = ?", c).First(&countyRecord).Error; err != nil {
					log.Printf("ERROR: Failed to look up county for tile generation: %v", err)
				} else {
					// Generate tiles
					if err := tiles.GenerateTilesForCounty(db.DB, countyRecord.ID, c, *minZoom, *maxZoom, *logging); err != nil {
						log.Printf("ERROR: Tile generation failed: %v", err)
					} else {
						log.Printf("Tile generation completed successfully for %s county!", c)
					}
				}
			}

			// Wait before processing the next county (if there is one)
			if i < len(counties)-1 {
				delayMin := 2 + rand.Intn(3)  // 2-4 minutes
				delaySeconds := rand.Intn(60) // 0-59 seconds
				totalDelay := time.Duration(delayMin)*time.Minute + time.Duration(delaySeconds)*time.Second

				fmt.Printf("⏳ Waiting %v before next county...\n", totalDelay)
				if cleanup != nil {
					cleanup() // Close current log file before waiting
				}
				time.Sleep(totalDelay)
			} else {
				// Last iteration
				if cleanup != nil {
					cleanup()
				}
			}
		}

		os.Exit(0)
	}

	// Check if we should run the enricher instead of starting the server
	if *enrichParcels {
		if *county == "" {
			log.Fatal("Error: --county flag is required when using --enrich-parcels")
		}

		// Split counties by comma
		counties := strings.Split(*county, ",")
		for i, c := range counties {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}

			// Set up file logging if enabled
			var cleanup func()
			if *logging {
				var err error
				cleanup, err = utils.SetupFileLogger(c, "enrich")
				if err != nil {
					log.Printf("Warning: Failed to set up file logging: %v", err)
				}
			}

			// Start enrichment
			if err := importers.StartParcelEnricher(db.DB, c, *resume, *maxParcels, *logging); err != nil {
				log.Printf("ERROR: Parcel enrichment failed for %s: %v", c, err)

				errStr := strings.ToLower(err.Error())
				if strings.Contains(errStr, "403") || strings.Contains(errStr, "forbidden") {
					log.Println("🛑 BLOCKING DETECTED (VPN/Firewall challenge). Stopping enrichment process.")
					if cleanup != nil {
						cleanup()
					}
					os.Exit(1)
				}
			} else {
				log.Printf("Parcel enrichment completed successfully for %s!", c)
			}

			// Wait before processing next county
			if i < len(counties)-1 {
				delayMin := 2 + rand.Intn(3)
				delaySeconds := rand.Intn(60)
				totalDelay := time.Duration(delayMin)*time.Minute + time.Duration(delaySeconds)*time.Second

				fmt.Printf("⏳ Waiting %v before next county...\n", totalDelay)
				if cleanup != nil {
					cleanup()
				}
				time.Sleep(totalDelay)
			} else {
				if cleanup != nil {
					cleanup()
				}
			}
		}

		os.Exit(0)
	}

	// Build/refresh parcel_search projection table.
	if *buildParcelSearch {
		if *county == "" {
			log.Fatal("Error: --county flag is required when using --build-parcel-search (use 'all' for statewide)")
		}

		counties := strings.Split(*county, ",")
		for i, c := range counties {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}

			var cleanup func()
			if *logging {
				var err error
				cleanup, err = utils.SetupFileLogger(c, "search")
				if err != nil {
					log.Printf("Warning: Failed to set up file logging: %v", err)
				}
			}

			if err := importers.StartParcelSearchBuilder(db.DB, c, *resume, *maxParcels); err != nil {
				log.Printf("ERROR: parcel_search build failed for %s: %v", c, err)
				if cleanup != nil {
					cleanup()
				}
				os.Exit(1)
			}
			log.Printf("parcel_search build completed successfully for %s!", c)

			if i < len(counties)-1 {
				delayMin := 1 + rand.Intn(2)  // 1-2 minutes
				delaySeconds := rand.Intn(60) // 0-59 seconds
				totalDelay := time.Duration(delayMin)*time.Minute + time.Duration(delaySeconds)*time.Second
				fmt.Printf("Waiting %v before next county...\n", totalDelay)
				if cleanup != nil {
					cleanup()
				}
				time.Sleep(totalDelay)
			} else if cleanup != nil {
				cleanup()
			}
		}

		os.Exit(0)
	}

	// Build/refresh materialized owner groups table.
	if *buildOwnerGroups {
		if *county == "" {
			log.Fatal("Error: --county flag is required when using --build-owner-groups (use 'all' for statewide)")
		}

		counties := strings.Split(*county, ",")
		for i, c := range counties {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}

			var cleanup func()
			if *logging {
				var err error
				cleanup, err = utils.SetupFileLogger(c, "owner_groups")
				if err != nil {
					log.Printf("Warning: Failed to set up file logging: %v", err)
				}
			}

			if err := importers.StartOwnerGroupsBuilder(db.DB, c, *resume, *maxParcels); err != nil {
				log.Printf("ERROR: owner_groups build failed for %s: %v", c, err)
				if cleanup != nil {
					cleanup()
				}
				os.Exit(1)
			}
			log.Printf("owner_groups build completed successfully for %s!", c)

			if i < len(counties)-1 {
				delayMin := 1 + rand.Intn(2)  // 1-2 minutes
				delaySeconds := rand.Intn(60) // 0-59 seconds
				totalDelay := time.Duration(delayMin)*time.Minute + time.Duration(delaySeconds)*time.Second
				fmt.Printf("Waiting %v before next county...\n", totalDelay)
				if cleanup != nil {
					cleanup()
				}
				time.Sleep(totalDelay)
			} else if cleanup != nil {
				cleanup()
			}
		}

		os.Exit(0)
	}

	if *importTaxes {
		if *county == "" {
			log.Fatal("Error: --county flag is required when using --import-taxes")
		}

		// Set up file logging if enabled
		if *logging {
			cleanup, err := utils.SetupFileLogger(*county, "taxes")
			if err != nil {
				log.Printf("Warning: Failed to set up file logging: %v", err)
			} else {
				defer cleanup()
			}
		}

		if err := importers.StartTaxImporter(db.DB, *county, *resume, *maxParcels, *logging); err != nil {
			log.Printf("ERROR: Parcel tax import failed: %v", err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	if *importCounties {
		// Set up file logging if enabled
		if *logging {
			cleanup, err := utils.SetupFileLogger("all", "counties")
			if err != nil {
				log.Printf("Warning: Failed to set up file logging: %v", err)
			} else {
				defer cleanup()
			}
		}

		log.Println("Starting county boundary import from SAGIS API...")
		if err := importers.StartCountyImporter(db.DB); err != nil {
			log.Printf("County import completed with errors: %v", err)
			os.Exit(1)
		}
		log.Println("County import completed successfully!")
		os.Exit(0)
	}

	// Initialize the Parcel tile cache
	tiles.InitParcelTilesCache()
	// Initialize tax heatmap/tax parcel tile caches
	tiles.InitTaxTilesCache()
	// Preload default county tax heatmap stats for all available years.
	if err := handlers.PreloadTaxHeatmapStatsForCounty(671); err != nil {
		log.Printf("WARNING: Failed preloading tax heatmap stats cache for county 671: %v", err)
	} else {
		log.Printf("Tax heatmap stats cache preloaded for county 671")
	}

	// Initialize PMTiles cache
	if err := tiles.InitPMTilesCache(); err != nil {
		log.Fatalf("ERROR: Failed to initialize PMTiles cache: %v", err)
	}

	// Initialize county tiles cache and pre-warm with zoom 6-8 tiles
	tiles.CountyTilesCache = &sync.Map{}
	go func() {
		var countyTiles []struct {
			Z    int
			X    int
			Y    int
			Data []byte
		}
		// Load zoom 6-8 county tiles (covers Georgia at most common view levels)
		if err := db.DB.Raw(`
			SELECT z, x, y, data FROM tiles 
			WHERE layer = 'counties' AND z BETWEEN 6 AND 8
		`).Scan(&countyTiles).Error; err != nil {
			log.Printf("WARNING: Failed to pre-warm county tiles cache: %v", err)
			return
		}
		for _, t := range countyTiles {
			cacheKey := fmt.Sprintf("%d/%d/%d", t.Z, t.X, t.Y)
			tiles.CountyTilesCache.Store(cacheKey, t.Data)
		}
		log.Printf("County tiles cache pre-warmed with %d tiles (zoom 6-8)", len(countyTiles))
	}()

	// Pre-warm PMTiles cache with commonly accessed ranges
	go func() {
		file, err := os.Open("./tiles/georgia.pmtiles")
		if err != nil {
			log.Printf("WARNING: Failed to open PMTiles for pre-warming: %v", err)
			return
		}
		defer file.Close()

		// Ranges from initial page load
		ranges := []struct {
			start int64
			end   int64
		}{
			{0, 16383},         // Header
			{1595, 10967},      // Root directory
			{1165912, 1234936}, // Tile data
			{1234937, 1308328},
			{1308329, 1442572},
			{1442573, 1522203},
			{1522204, 1615794},
			{1615795, 1694735},
			{1694736, 1789082},
			{1789083, 1919147},
			{1919148, 1956223},
		}

		for _, r := range ranges {
			size := r.end - r.start + 1
			data := make([]byte, size)

			_, err := file.ReadAt(data, r.start)
			if err != nil && err != io.EOF {
				log.Printf("WARNING: Failed to pre-warm range %d-%d: %v", r.start, r.end, err)
				continue
			}

			headerRange := fmt.Sprintf("bytes=%d-%d", r.start, r.end)
			if tiles.PMTilesCache != nil {
				atomic.AddUint64(&tiles.PMTilesSize, uint64(size))
				tiles.PMTilesCache.Add(headerRange, data)
			}
		}

		log.Printf("PMTiles cache pre-warmed with %d ranges", len(ranges))
	}()

	// Create a new Gin router
	router := gin.Default()

	// Enable CORS for all origins (adjust as needed for production)
	router.Use(cors.Default())

	// Create gzip middleware
	gzipMiddleware := gzip.Gzip(
		gzip.DefaultCompression,
		gzip.WithExcludedPaths([]string{
			"/georgia.pmtiles",
		}),
	)

	// Wrap gzip middleware to also exclude /api/tiles/ prefix (pre-compressed MVT tiles)
	router.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/tiles/") {
			c.Next()
			return
		}
		gzipMiddleware(c)
	})

	// Serve static files (index.html, app.js, etc.)
	router.StaticFile("/", "./index.html")
	router.StaticFile("/index.html", "./index.html")

	router.GET("/georgia.pmtiles", handlers.ServePMTilesWithCache)

	// Serve static assets (lib/fonts/sprites) with aggressive caching.
	router.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/assets/") {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		}
		c.Next()
	})
	router.Static("/assets", "./static")

	// Serve JavaScript modules and style manifests.
	router.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/js/") && strings.HasSuffix(c.Request.URL.Path, ".js") {
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
		}
		c.Next()
	})
	router.Static("/js", "./js")
	router.Static("/styles", "./styles")

	// Register API routes
	api := router.Group("/api")
	{
		// Vector tile endpoints for pre-generated MVT tiles
		api.GET("/tiles/:z/:x/:y", handlers.GetVectorTile)          // Parcel tiles
		api.GET("/tiles/counties/:z/:x/:y", handlers.GetCountyTile) // County tiles
		api.GET("/tiles/tax-heatmap/:z/:x/:y", handlers.GetTaxHeatmapTile)
		api.GET("/tiles/tax-parcels/:z/:x/:y", handlers.GetTaxParcelTile)
		api.GET("/tax-heatmap/stats", handlers.GetTaxHeatmapStats)
		api.POST("/tax-heatmap/parcel-values", handlers.GetParcelTaxValuesBatch)
		api.GET("/parcels/:feature_id", handlers.GetParcelDetails) // Live parcel details for popup hydration
		api.GET("/parcels/:feature_id/taxes", handlers.GetParcelTaxHistory)
		api.POST("/parcels/previews", handlers.GetParcelPreviews)
		api.GET("/owners/properties", handlers.GetOwnerProperties)
		api.GET("/counties/:id/centroid", handlers.GetCountyCentroid)
		api.GET("/search/parcels", handlers.SearchParcels) // Parcel address search/autocomplete

		// Cache stats endpoint
		api.GET("/cache/stats", handlers.GetCacheStats)
	}

	// Start the server
	log.Println("Parcel Heatmap server starting on port 9000")
	router.Run(":9000")
}
