-- Reduce storage width for search projection coordinates and remove write-only fields.
-- Run during a low-traffic window: ALTER COLUMN TYPE rewrites each table.

ALTER TABLE parcel_search
DROP COLUMN IF EXISTS source,
DROP COLUMN IF EXISTS confidence;

ALTER TABLE parcel_search
ALTER COLUMN lat TYPE real USING lat::real,
ALTER COLUMN lng TYPE real USING lng::real;

ALTER TABLE parcels
ALTER COLUMN search_lat TYPE real USING search_lat::real,
ALTER COLUMN search_lng TYPE real USING search_lng::real;
