package main

import (
	"flag"
	"log"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/renderyourworld/parcel_heatmap/db"
	"github.com/renderyourworld/parcel_heatmap/handlers"
	"github.com/renderyourworld/parcel_heatmap/importers"
)

func main() {
	// Parse command-line flags
	importParcels := flag.Bool("import-parcels", false, "Run parcel importer for specified county")
	county := flag.String("county", "", "County name to import parcels for")
	resume := flag.Bool("resume", false, "Resume import from last checkpoint")
	maxParcels := flag.Int("max-parcels", 0, "Maximum number of parcels to import (0 = no limit, for testing)")
	flag.Parse()

	// Load environment variables from .env file (if it exists)
	_ = godotenv.Load()

	// Initialize the database connection
	db.Connect()

	// Check if we should run the importer instead of starting the server
	if *importParcels {
		if *county == "" {
			log.Fatal("Error: --county flag is required when using --import-parcels")
		}

		if *maxParcels > 0 {
			log.Printf("Starting parcel import for %s county (resume=%v, max=%d)", *county, *resume, *maxParcels)
		} else {
			log.Printf("Starting parcel import for %s county (resume=%v)", *county, *resume)
		}

		if err := importers.StartParcelImporter(db.DB, *county, *resume, *maxParcels); err != nil {
			log.Printf("ERROR: Parcel import failed: %v", err)
			os.Exit(1)
		}

		log.Println("Parcel import completed successfully!")
		os.Exit(0)
	}

	// Optional: Start the Georgia County importer to populate county boundaries
	// Uncomment the lines below to import all 159 counties from SAGIS API (~2-3 minutes)
	// Then comment back out to prevent re-importing on every restart
	//
	// log.Println("Starting county boundary import from SAGIS API...")
	// if err := importers.StartCountyImporter(db.DB); err != nil {
	// 	log.Printf("County import completed with errors: %v", err)
	// } else {
	// 	log.Println("County import completed successfully!")
	// }

	// Create a new Gin router
	router := gin.Default()

	// Enable CORS for all origins (adjust as needed for production)
	router.Use(cors.Default())

	// Enable GZIP compression for responses
	router.Use(gzip.Gzip(gzip.DefaultCompression))

	// Register API routes
	api := router.Group("/api")
	{
		// County boundary endpoint with zoom-aware detail switching
		api.GET("/counties", handlers.GetCountyBoundaries)

		// Parcel endpoint for bbox-based queries with precomputed GeoJSON
		api.GET("/parcels", handlers.GetParcels)
	}

	// Health check endpoint
	router.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	// Start the server
	log.Println("Parcel Heatmap server starting at http://localhost:9000")
	router.Run(":9000")
}
