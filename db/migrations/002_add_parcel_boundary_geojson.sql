-- Migration: Add boundary_geojson, error tracking, and checkpoint table for parcel importer
-- This migration prepares the parcels table for the resumable import process

-- Rename centroid_geojson to centroid for consistency with counties table
ALTER TABLE parcels RENAME COLUMN centroid_geojson TO centroid;

-- Add boundary_geojson JSONB column for precomputed GeoJSON (like counties)
ALTER TABLE parcels ADD COLUMN IF NOT EXISTS boundary_geojson jsonb;

-- Add error_message column for tracking import failures
ALTER TABLE parcels ADD COLUMN IF NOT EXISTS error_message text;

-- Drop duplicate object_id column (keep objectid)
ALTER TABLE parcels DROP COLUMN IF EXISTS object_id;

-- Make site_address and classification nullable (many parcels lack these)
ALTER TABLE parcels ALTER COLUMN site_address DROP NOT NULL;
ALTER TABLE parcels ALTER COLUMN classification DROP NOT NULL;

-- Create trigger function to auto-populate boundary_geojson from geometry
CREATE OR REPLACE FUNCTION refresh_parcel_boundary_geojson()
RETURNS TRIGGER AS $$
BEGIN
    -- Generate GeoJSON Feature from geometry column
    IF NEW.geometry IS DISTINCT FROM OLD.geometry THEN
        NEW.boundary_geojson := jsonb_build_object(
            'type', 'Feature',
            'geometry', ST_AsGeoJSON(NEW.geometry, 6)::jsonb,
            'properties', jsonb_build_object(
                'parcel_id', NEW.parcel_id,
                'site_address', NEW.site_address,
                'site_number', NEW.site_number,
                'owner_name', NEW.owner_name,
                'owner_address', NEW.owner_address,
                'acres', NEW.acres,
                'classification', NEW.classification,
                'tax_district', NEW.tax_district
            )
        );
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to auto-update boundary_geojson on geometry changes
DROP TRIGGER IF EXISTS refresh_parcel_boundary_geojson_trigger ON parcels;
CREATE TRIGGER refresh_parcel_boundary_geojson_trigger
BEFORE INSERT OR UPDATE OF geometry ON parcels
FOR EACH ROW
EXECUTE FUNCTION refresh_parcel_boundary_geojson();

-- Create import_checkpoints table for resumable import tracking
CREATE TABLE IF NOT EXISTS import_checkpoints (
    id SERIAL PRIMARY KEY,
    county_name VARCHAR(50) NOT NULL UNIQUE,
    last_processed_id BIGINT DEFAULT 0,
    status VARCHAR(20) DEFAULT 'RUNNING',
    start_time TIMESTAMP,
    end_time TIMESTAMP,
    total_processed INT DEFAULT 0,
    total_failed INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Add index on county_name for quick lookups
CREATE INDEX IF NOT EXISTS idx_checkpoint_county ON import_checkpoints(county_name);

-- Add index on status for monitoring active imports
CREATE INDEX IF NOT EXISTS idx_checkpoint_status ON import_checkpoints(status);

-- Backfill boundary_geojson for existing parcels (if any)
UPDATE parcels
SET boundary_geojson = jsonb_build_object(
    'type', 'Feature',
    'geometry', ST_AsGeoJSON(geometry, 6)::jsonb,
    'properties', jsonb_build_object(
        'parcel_id', parcel_id,
        'site_address', site_address,
        'site_number', site_number,
        'owner_name', owner_name,
        'owner_address', owner_address,
        'acres', acres,
        'classification', classification,
        'tax_district', tax_district
    )
)
WHERE boundary_geojson IS NULL AND geometry IS NOT NULL;
