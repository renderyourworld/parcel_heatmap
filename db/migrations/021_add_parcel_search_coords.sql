-- Precompute parcel search coordinates in WGS84 for fast address search responses.
ALTER TABLE parcels
ADD COLUMN IF NOT EXISTS search_lat double precision,
ADD COLUMN IF NOT EXISTS search_lng double precision;

CREATE OR REPLACE FUNCTION refresh_parcel_search_coords()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.geometry IS NULL THEN
        NEW.search_lat := NULL;
        NEW.search_lng := NULL;
    ELSE
        NEW.search_lat := ST_Y(ST_Transform(ST_PointOnSurface(NEW.geometry), 4326));
        NEW.search_lng := ST_X(ST_Transform(ST_PointOnSurface(NEW.geometry), 4326));
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS refresh_parcel_search_coords_trigger ON parcels;

CREATE TRIGGER refresh_parcel_search_coords_trigger
BEFORE INSERT OR UPDATE OF geometry ON parcels
FOR EACH ROW
EXECUTE FUNCTION refresh_parcel_search_coords();

-- Backfill existing rows.
UPDATE parcels
SET
    search_lat = ST_Y(ST_Transform(ST_PointOnSurface(geometry), 4326)),
    search_lng = ST_X(ST_Transform(ST_PointOnSurface(geometry), 4326))
WHERE geometry IS NOT NULL
  AND (search_lat IS NULL OR search_lng IS NULL);
