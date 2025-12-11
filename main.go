package main

import (
	"log"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/renderyourworld/parcel_heatmap/db"
	"github.com/renderyourworld/parcel_heatmap/handlers"
	// Uncomment the next line to run the initial county import from SAGIS API
	// "github.com/renderyourworld/parcel_heatmap/importers"
)

func main() {
	// Load environment variables from .env file (if it exists)
	_ = godotenv.Load()

	// Initialize the database connection
	db.Connect()

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

	// Register route for getting visible parcels
	api := router.Group("/api")
	{
		// County boundary endpoint with zoom-aware detail switching
		api.GET("/counties", handlers.GetCountyBoundaries)

		// Parcel endpoint for bbox-based queries
		api.GET("/parcels", handlers.GetVisibleParcels)
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
