-- Migration: Clean up redundant and incorrect indexes

-- PARCELS TABLE
-- Remove idx_county_parcel: WRONG - allows duplicate parcel_ids across different counties
-- Parcel IDs should only be unique within a county, not globally
DROP INDEX IF EXISTS idx_county_parcel;

-- Remove idx_parcel_pin: Redundant with idx_parcels_pin (county_id, parcel_id)
-- Composite index on (county_id, parcel_id) can be used for queries on county_id alone
DROP INDEX IF EXISTS idx_parcel_pin;

-- Remove idx_parcels_unprocessed: Only useful for monitoring failed imports
-- Not used during normal import or query operations
DROP INDEX IF EXISTS idx_parcels_unprocessed;

-- COUNTIES TABLE
-- Remove idx_counties_simplified_geom: Duplicate of idx_counties_boundary_simplified
-- Both index the same column (boundary_simplified) with identical GIST indexes
DROP INDEX IF EXISTS idx_counties_simplified_geom;
