package utils

import (
	"math"
)

// TileCoord represents a single tile in the XYZ coordinate system
type TileCoord struct {
	Z int // Zoom level
	X int // Tile X coordinate
	Y int // Tile Y coordinate
}

// BBox represents a bounding box in WGS84 coordinates
type BBox struct {
	MinLon float64
	MinLat float64
	MaxLon float64
	MaxLat float64
}

// TileToBBox converts tile coordinates (Z/X/Y) to a WGS84 bounding box
// Returns (minLon, minLat, maxLon, maxLat)
func TileToBBox(z, x, y int) BBox {
	n := math.Pow(2, float64(z))
	
	minLon := float64(x)/n*360.0 - 180.0
	maxLon := float64(x+1)/n*360.0 - 180.0
	
	minLat := tileYToLat(y+1, z)
	maxLat := tileYToLat(y, z)
	
	return BBox{
		MinLon: minLon,
		MinLat: minLat,
		MaxLon: maxLon,
		MaxLat: maxLat,
	}
}

// tileYToLat converts tile Y coordinate to latitude
func tileYToLat(y, z int) float64 {
	n := math.Pow(2, float64(z))
	latRad := math.Atan(math.Sinh(math.Pi * (1 - 2*float64(y)/n)))
	return latRad * 180.0 / math.Pi
}

// LonToTileX converts longitude to tile X coordinate at given zoom
func LonToTileX(lon float64, z int) int {
	n := math.Pow(2, float64(z))
	return int(math.Floor((lon + 180.0) / 360.0 * n))
}

// LatToTileY converts latitude to tile Y coordinate at given zoom
func LatToTileY(lat float64, z int) int {
	n := math.Pow(2, float64(z))
	latRad := lat * math.Pi / 180.0
	return int(math.Floor((1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * n))
}

// GetTilesForBounds returns all tile coordinates that intersect the given bounding box
// for zoom levels from minZoom to maxZoom (inclusive)
func GetTilesForBounds(bounds BBox, minZoom, maxZoom int) []TileCoord {
	var tiles []TileCoord
	
	for z := minZoom; z <= maxZoom; z++ {
		// Calculate tile range for this zoom level
		minTileX := LonToTileX(bounds.MinLon, z)
		maxTileX := LonToTileX(bounds.MaxLon, z)
		minTileY := LatToTileY(bounds.MaxLat, z) // Note: Y is inverted
		maxTileY := LatToTileY(bounds.MinLat, z)
		
		// Generate all tiles in the range
		for x := minTileX; x <= maxTileX; x++ {
			for y := minTileY; y <= maxTileY; y++ {
				tiles = append(tiles, TileCoord{Z: z, X: x, Y: y})
			}
		}
	}
	
	return tiles
}

// CountTilesForBounds returns the number of tiles for a given bounds and zoom range
// Useful for progress estimation without generating the full list
func CountTilesForBounds(bounds BBox, minZoom, maxZoom int) int {
	count := 0
	
	for z := minZoom; z <= maxZoom; z++ {
		minTileX := LonToTileX(bounds.MinLon, z)
		maxTileX := LonToTileX(bounds.MaxLon, z)
		minTileY := LatToTileY(bounds.MaxLat, z)
		maxTileY := LatToTileY(bounds.MinLat, z)
		
		width := maxTileX - minTileX + 1
		height := maxTileY - minTileY + 1
		count += width * height
	}
	
	return count
}
