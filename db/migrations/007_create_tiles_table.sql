-- Migration: Create tiles table for pre-generated vector tiles
-- This table stores pre-computed MVT (Mapbox Vector Tile) binary data
-- for fast serving of parcel geometry at various zoom levels (16-19)

CREATE TABLE tiles (
    z SMALLINT NOT NULL CHECK (z >= 0 AND z <= 30),
    x INTEGER NOT NULL CHECK (x >= 0),
    y INTEGER NOT NULL CHECK (y >= 0),
    county_id SMALLINT NOT NULL,
    layer VARCHAR(50) NOT NULL DEFAULT 'parcels',
    data BYTEA NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (z, x, y, county_id, layer)
);

-- Index for fast tile lookup (most common query pattern)
CREATE INDEX idx_tiles_lookup ON tiles(z, x, y, layer);

-- Index for county-based operations (regenerate tiles for specific county)
CREATE INDEX idx_tiles_county ON tiles(county_id);

-- Add foreign key constraint to counties table
ALTER TABLE tiles
ADD CONSTRAINT tiles_county_id_fkey 
FOREIGN KEY (county_id) REFERENCES counties(id) ON DELETE CASCADE;

-- Comments for documentation
COMMENT ON TABLE tiles IS 'Pre-generated vector tiles (MVT/PBF format)';
COMMENT ON COLUMN tiles.z IS 'Zoom level';
COMMENT ON COLUMN tiles.x IS 'Tile X coordinate';
COMMENT ON COLUMN tiles.y IS 'Tile Y coordinate';
COMMENT ON COLUMN tiles.county_id IS 'County this tile belongs to';
COMMENT ON COLUMN tiles.layer IS 'Layer name (parcels, counties, etc.)';
COMMENT ON COLUMN tiles.data IS 'Gzipped MVT binary data';
