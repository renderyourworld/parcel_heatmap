-- Migration: Remove legacy columns from county_field_mappings table
-- is_geometry: no longer needed since geometry is handled separately from properties mapping
-- data_type: unused - target column name determines type conversion logic
-- is_pin: redundant - target_column == "parcel_id" provides same information

ALTER TABLE county_field_mappings
DROP COLUMN IF EXISTS is_geometry,
DROP COLUMN IF EXISTS data_type,
DROP COLUMN IF EXISTS is_pin;
