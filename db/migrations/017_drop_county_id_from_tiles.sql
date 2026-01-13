-- Migration 017: Drop county_id from tiles table
-- This column is no longer needed since tiles can contain parcels from multiple counties.

-- Drop the index on county_id if it still exists
DROP INDEX IF EXISTS idx_tiles_county;

-- Drop the column
ALTER TABLE tiles DROP COLUMN IF EXISTS county_id;
