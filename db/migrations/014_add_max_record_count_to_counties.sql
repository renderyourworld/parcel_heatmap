-- Migration: Add max_record_count to counties
-- Description: Adds a column to store the maximum records an API request can return for a specific county.

ALTER TABLE counties 
ADD COLUMN max_record_count INT DEFAULT 1000;

-- Optional: Seed known values if desired
-- UPDATE counties SET max_record_count = 1000 WHERE name = 'Cobb';
-- UPDATE counties SET max_record_count = 15000 WHERE name = 'Forsyth';
