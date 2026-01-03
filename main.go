package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"os"
	atomic "sync/atomic"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/renderyourworld/parcel_heatmap/db"
	"github.com/renderyourworld/parcel_heatmap/handlers"
	"github.com/renderyourworld/parcel_heatmap/importers"
	"github.com/renderyourworld/parcel_heatmap/models"
	"github.com/renderyourworld/parcel_heatmap/tiles"
)

func main() {
	mime.AddExtensionType(".pmtiles", "application/vnd.pmtiles")

	// Parse command-line flags
	importParcels := flag.Bool("import-parcels", false, "Run parcel importer for specified county")
	importTaxes := flag.Bool("import-taxes", false, "Run parcel tax importer for specified county")
	importCounties := flag.Bool("import-counties", false, "Import all Georgia county boundaries from SAGIS API")
	county := flag.String("county", "", "County name to import parcels for")
	resume := flag.Bool("resume", false, "Resume import from last checkpoint")
	maxParcels := flag.Int("max-parcels", 0, "Maximum number of parcels to import (0 = no limit, for testing)")
	skipTiles := flag.Bool("skip-tiles", false, "Skip tile generation after import (for testing)")

	generateTiles := flag.Bool("generate-tiles", false, "Generate vector tiles for existing parcels")
	minZoom := flag.Int("min-zoom", 13, "Minimum zoom level for tile generation")
	maxZoom := flag.Int("max-zoom", 19, "Maximum zoom level for tile generation")

	logPerf := flag.Bool("logPerf", false, "Enable performance logging (time elapsed, transactions per second)")

	flag.Parse()

	// Load environment variables from .env file (if it exists)
	_ = godotenv.Load()

	// Initialize the database connection
	db.Connect()

	// Check if we should generate tiles for existing parcels
	if *generateTiles {
		if *county == "" {
			log.Fatal("Error: --county flag is required when using --generate-tiles")
		}

		log.Printf("Generating tiles for %s county (zoom %d-%d)...", *county, *minZoom, *maxZoom)

		// Look up county
		var countyRecord models.County
		if err := db.DB.Where("name = ?", *county).First(&countyRecord).Error; err != nil {
			log.Fatalf("ERROR: County '%s' not found: %v", *county, err)
		}

		// Generate tiles
		if err := tiles.GenerateTilesForCounty(db.DB, countyRecord.ID, *county, *minZoom, *maxZoom, *logPerf); err != nil {
			log.Fatalf("ERROR: Tile generation failed: %v", err)
		}

		log.Println("Tile generation completed successfully!")
		os.Exit(0)
	}

	// Check if we should run the importer instead of starting the server
	if *importParcels {
		if *county == "" {
			log.Fatal("Error: --county flag is required when using --import-parcels")
		}

		if *maxParcels > 0 {
			log.Printf("Starting parcel import for %s county (resume=%v, max=%d, skip-tiles=%v)", *county, *resume, *maxParcels, *skipTiles)
		} else {
			log.Printf("Starting parcel import for %s county (resume=%v, skip-tiles=%v)", *county, *resume, *skipTiles)
		}

		if err := importers.StartParcelImporter(db.DB, *county, *resume, *maxParcels, *logPerf); err != nil {
			log.Printf("ERROR: Parcel import failed: %v", err)
			os.Exit(1)
		}

		log.Println("Parcel import completed successfully!")

		// Generate tiles after successful import (unless skipped)
		if !*skipTiles {
			log.Printf("Generating tiles for %s county (zoom %d-%d)...", *county, *minZoom, *maxZoom)

			// Look up county ID
			var countyRecord models.County
			if err := db.DB.Where("name = ?", *county).First(&countyRecord).Error; err != nil {
				log.Printf("ERROR: Failed to look up county for tile generation: %v", err)
				os.Exit(1)
			}

			// Generate tiles
			if err := tiles.GenerateTilesForCounty(db.DB, countyRecord.ID, *county, *minZoom, *maxZoom, *logPerf); err != nil {
				log.Printf("ERROR: Tile generation failed: %v", err)
				os.Exit(1)
			}

			log.Println("Tile generation completed successfully!")
		}

		os.Exit(0)
	}

	if *importTaxes {
		if *county == "" {
			log.Fatal("Error: --county flag is required when using --import-taxes")
		}
		if err := importers.StartTaxImporter(db.DB, *county, *resume, *maxParcels, *logPerf); err != nil {
			log.Printf("ERROR: Parcel tax import failed: %v", err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	if *importCounties {
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
