-- Migration: Update constraints to support county-agnostic content
-- This migration ensures that each (z, x, y, layer) has only one record in the database.

-- Drop constraints that involve county_id
ALTER TABLE tiles DROP CONSTRAINT tiles_pkey;
ALTER TABLE tiles DROP CONSTRAINT tiles_county_id_fkey;

-- Update the table schema: county_id is now just metadata
ALTER TABLE tiles ALTER COLUMN county_id DROP NOT NULL;

-- Add the new primary key (without county_id)
ALTER TABLE tiles ADD PRIMARY KEY (z, x, y, layer);
