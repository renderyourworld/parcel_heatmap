-- Migration 009: Change unique constraint from (county_id, parcel_id) to (county_id, objectid)
-- Reason: ArcGIS servers can have duplicate parcel_ids with different objectids and geometries
-- This allows storing all duplicate parcels as separate records

-- Drop the existing unique constraint on (county_id, parcel_id)
ALTER TABLE parcels DROP CONSTRAINT IF EXISTS parcels_county_id_parcel_id_key;
ALTER TABLE parcels DROP CONSTRAINT IF EXISTS idx_county_parcel;

-- Add new unique constraint on (county_id, objectid)
-- OBJECTID is the true unique identifier from ArcGIS
ALTER TABLE parcels ADD CONSTRAINT parcels_county_id_objectid_key UNIQUE (county_id, objectid);

-- Add a non-unique index on (county_id, parcel_id) for efficient lookups
-- This allows querying by parcel_id while allowing duplicates
CREATE INDEX IF NOT EXISTS idx_parcels_county_parcel ON parcels (county_id, parcel_id);