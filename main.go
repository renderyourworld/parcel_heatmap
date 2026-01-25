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
	importTaxes := flag.Bool("import-taxes", false, "Run parcel tax importer for specified county")
	importCounties := flag.Bool("import-counties", false, "Import all Georgia county boundaries from SAGIS API")
	county := flag.String("county", "", "County name to import parcels for")
	resume := flag.Bool("resume", false, "Resume import from last checkpoint")
	maxParcels := flag.Int("max-parcels", 0, "Maximum number of parcels to import (0 = no limit, for testing)")
	skipTiles := flag.Bool("skip-tiles", false, "Skip tile generation after import (for testing)")

	generateTiles := flag.Bool("generate-tiles", false, "Generate vector tiles for existing parcels")
	minZoom := flag.Int("min-zoom", 13, "Minimum zoom level for tile generation")
	maxZoom := flag.Int("max-zoom", 19, "Maximum zoom level for tile generation")

	benchmark := flag.Bool("benchmark", false, "Run performance benchmark test using chromedp")
	benchmarkLighthouse := flag.Bool("benchmark-lighthouse", false, "Run Lighthouse audit")
	benchmarkURL := flag.String("benchmark-url", "http://localhost:9000", "URL to benchmark")
	benchmarkMode := flag.String("benchmark-mode", "desktop", "Benchmark mode: mobile or desktop")

	logging := flag.Bool("log", false, "Enable logging to file in logs/ directory")

	flag.Parse()

	// Load environment variables from .env file (if it exists)
	_ = godotenv.Load()

	// Initialize the database connection
	db.Connect()

	// Check if we should run performance benchmark
	if *benchmark {
		now := time.Now()
		// Set up dual logging to terminal and .log file EARLY to capture all output
		cleanup, err := benchmarks.SetupBenchmarkLogger(now)
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

			log.Printf("Starting parcel enrichment for %s county (resume=%v)", c, *resume)

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

	// Initialize PMTiles cache
	if err := tiles.InitPMTilesCache(); err != nil {
		log.Fatalf("ERROR: Failed to initialize PMTiles cache: %v", err)
	}

	// Pre-warm PMTiles cache (first 100KB)
	go func() {
		file, err := os.Open("./tiles/georgia.pmtiles")
		if err == nil {
			defer file.Close()
			limit := 100 * 1024
			data := make([]byte, limit)
			n, err := file.Read(data)
			if err == nil || err == io.EOF {
				if n > 0 {
					headerRange := fmt.Sprintf("bytes=0-%d", n-1)
					if tiles.PMTilesCache != nil {
						atomic.AddUint64(&tiles.PMTilesSize, uint64(n))
						tiles.PMTilesCache.Add(headerRange, data[:n])
					}
					log.Printf("PMTiles cache pre-warmed with first %d bytes", n)
				}
			} else {
				log.Printf("WARNING: Failed to read PMTiles for pre-warming: %v", err)
			}
		} else {
			log.Printf("WARNING: Failed to open PMTiles for pre-warming: %v", err)
		}
	}()

	// Preload county boundary data
	if err := handlers.LoadCountyBoundaries(db.DB); err != nil {
		log.Fatalf("ERROR: Failed to load county boundaries: %v", err)
	}

	// Create a new Gin router
	router := gin.Default()

	// Enable CORS for all origins (adjust as needed for production)
	router.Use(cors.Default())

	// Exclude GZIP compression for pre-compressed endpoints and binary files
	router.Use(gzip.Gzip(
		gzip.DefaultCompression,
		gzip.WithExcludedPaths([]string{
			"/georgia.pmtiles",
			"/api/counties/simplified",
			"/api/counties/full",
		}),
	))

	// Serve static files (index.html, app.js, etc.)
	router.StaticFile("/", "./index.html")
	router.StaticFile("/index.html", "./index.html")

	// No cache for dev work
	router.GET("/app.js", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.File("./app.js")
	})

	router.StaticFile("/styles/light.json", "./styles/light.json")
	router.StaticFile("/styles/dark.json", "./styles/dark.json")
	router.GET("/georgia.pmtiles", handlers.ServePMTilesWithCache)

	// Serve fonts and sprites with aggressive caching
	router.Static("/fonts", "./static/fonts")
	router.Static("/sprites", "./static/sprites")
	router.Static("/lib", "./static/lib")

	// Register API routes
	api := router.Group("/api")
	{
		// County boundary endpoints (preloaded at startup)
		api.GET("/counties/simplified", handlers.GetSimplifiedCountyBoundaries)
		api.GET("/counties/full", handlers.GetFullCountyBoundaries)

		// Vector tile endpoint for pre-generated MVT tiles
		api.GET("/tiles/:z/:x/:y", handlers.GetVectorTile)

		// Cache stats endpoint
		api.GET("/cache/stats", handlers.GetCacheStats)
	}

	// Health check endpoint
	router.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	// Start the server
	log.Println("Parcel Heatmap server starting on port 9000")
	router.Run(":9000")
}
