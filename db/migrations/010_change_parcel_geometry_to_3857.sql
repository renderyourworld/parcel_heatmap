-- Migration: Change parcel geometry SRID from 4326 to 3857
-- This migration transforms existing geometries and updates the column constraint

-- Alter the column type to use 3857
ALTER TABLE parcels 
ALTER COLUMN geometry TYPE geometry(MultiPolygon, 3857)
USING ST_Transform(geometry, 3857);

-- Recreate the spatial index
REINDEX INDEX idx_parcels_geometry;
