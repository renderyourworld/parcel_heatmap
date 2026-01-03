-- Migration: Add import_type to import_checkpoints
-- Description: Adds import_type column to support multiple import types (parcel, tax) per county.

-- 1. Add import_type column with a default value of 'parcel'
ALTER TABLE import_checkpoints 
ADD COLUMN import_type VARCHAR(20) NOT NULL DEFAULT 'parcel';

-- 2. Drop the existing unique constraint on county_name
-- Note: 'import_checkpoints_county_name_key' is the standard GORM/Postgres name for a unique constraint on county_name
ALTER TABLE import_checkpoints 
DROP CONSTRAINT IF EXISTS import_checkpoints_county_name_key;

-- 3. Drop the redundant index if it exists
DROP INDEX IF EXISTS idx_checkpoint_county;

-- 4. Create the new unique index on (county_name, import_type)
-- This allows one record of each type per county
CREATE UNIQUE INDEX idx_county_import ON import_checkpoints (county_name, import_type);

-- 5. Update the default for future inserts (optional but good practice)
ALTER TABLE import_checkpoints 
ALTER COLUMN import_type DROP DEFAULT;
