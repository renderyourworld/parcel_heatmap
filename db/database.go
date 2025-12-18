package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Database connection instance
var DB *gorm.DB

// Connect to the PostGIS database.
// Note: Migrations are managed manually via SQL files in db/migrations/
func Connect() *gorm.DB {
	// Get environment variables for database connection
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")

	// Construct the DSN (Data Source Name)
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=UTC",
		host, user, password, dbname, port)

	// Open the connection using GORM
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Get the underlying *sql.DB for connection pool configuration
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("Failed to get database instance: %v", err)
	}

	// Configure connection pool for optimal performance
	sqlDB.SetMaxOpenConns(25)                      // Max concurrent connections (good for 16GB RAM)
	sqlDB.SetMaxIdleConns(5)                       // Keep 5 idle connections ready for bursts
	sqlDB.SetConnMaxLifetime(5 * time.Minute)      // Recycle connections after 5 minutes
	sqlDB.SetConnMaxIdleTime(1 * time.Minute)      // Close idle connections after 1 minute

	log.Println("Database connection established with connection pooling.")
	DB = db
	return DB
}
