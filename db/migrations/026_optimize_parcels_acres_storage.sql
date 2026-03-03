-- Reduce storage width for parcel acreage.
-- numeric is flexible but heavy for this use case; float8 is much smaller and
-- already matches application-level handling (float64).

ALTER TABLE parcels
ALTER COLUMN acres TYPE double precision USING acres::double precision;
