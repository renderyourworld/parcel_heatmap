-- Migration: Increase tax_district length
-- Description: Increases the varchar length from 10 to 50 to accommodate longer district names.

ALTER TABLE parcels 
ALTER COLUMN tax_district TYPE VARCHAR(50);
