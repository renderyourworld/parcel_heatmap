-- Migration: Remove boundary_geojson column and related trigger/function
-- This column is no longer needed as we're using pre-generated vector tiles instead of GeoJSON API
-- Centroid column is also removed as smart label placement is now handled in the tile rendering process

-- Drop trigger first
DROP TRIGGER IF EXISTS refresh_parcel_boundary_geojson_trigger ON parcels;

-- Drop function
DROP FUNCTION IF EXISTS refresh_parcel_boundary_geojson();

-- Drop column
ALTER TABLE parcels DROP COLUMN IF EXISTS boundary_geojson;
ALTER TABLE parcels DROP COLUMN IF EXISTS centroid;
