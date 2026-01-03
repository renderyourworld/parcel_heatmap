-- Migration: Remove extra assessed and fmv fields from parcel_taxes table

ALTER TABLE parcel_taxes 
-- Clean up old fields
DROP COLUMN IF EXISTS fmv_land,
DROP COLUMN IF EXISTS fmv_building,
DROP COLUMN IF EXISTS fmv_total,
DROP COLUMN IF EXISTS assessed_land,
DROP COLUMN IF EXISTS assessed_building,
DROP COLUMN IF EXISTS assessed_total,

-- Add new fields
ADD COLUMN appraised numeric(14,2),
ADD COLUMN assessed numeric(14,2),
ADD COLUMN millage numeric(8,6),
ADD COLUMN payer_name text;