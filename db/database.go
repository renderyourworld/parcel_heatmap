package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Database connection instances
var (
	DB   *gorm.DB      // GORM connection for general queries
	Pool *pgxpool.Pool // PgxPool for batch operations and high-performance inserts
)

// Connect to the PostGIS database.
// Initializes PgxPool and shares the connection pool with GORM
// Note: Migrations are managed manually via SQL files in db/migrations/
func Connect() *gorm.DB {
	// Get environment variables for database connection
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")

	// Construct the connection string for pgxpool
	connString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable&timezone=UTC",
		user, password, host, port, dbname)

	// Configure PgxPool with optimized settings
	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		log.Fatalf("Failed to parse pgxpool config: %v", err)
	}

	// Configure connection pool limits
	poolConfig.MaxConns = 25                        // Maximum number of connections in the pool
	poolConfig.MinConns = 15                        // Minimum number of idle connections to maintain
	poolConfig.MaxConnLifetime = 5 * time.Minute    // Recycle connections after 5 minutes
	poolConfig.MaxConnIdleTime = 1 * time.Minute    // Close idle connections after 1 minute
	poolConfig.HealthCheckPeriod = 30 * time.Second // Periodic health check interval

	// Create PgxPool connection
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		log.Fatalf("Failed to create pgxpool: %v", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("Failed to ping database via pgxpool: %v", err)
	}

	Pool = pool
	log.Println("PgxPool connection established (max: 25, min: 15)")

	// Create a *sql.DB from the PgxPool to share with GORM
	sqlDB := stdlib.OpenDBFromPool(pool)

	// Open GORM using the shared connection pool
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn: sqlDB, // Share the pgxpool connection
	}), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database via GORM: %v", err)
	}

	DB = db
	log.Println("GORM connection established using shared PgxPool")
	log.Println("Database ready: Single connection pool (max: 25, min: 15) shared by PgxPool and GORM")

	return DB
}
