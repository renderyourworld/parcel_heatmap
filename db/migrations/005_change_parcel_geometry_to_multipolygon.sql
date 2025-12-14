-- Change parcels geometry column from Polygon to MultiPolygon to support all geometry types
-- This matches the counties table structure

-- Drop the trigger that depends on the geometry column
DROP TRIGGER IF EXISTS refresh_parcel_boundary_geojson_trigger ON parcels;

-- Change the column type
ALTER TABLE parcels 
ALTER COLUMN geometry TYPE geometry(MultiPolygon, 4326) 
USING ST_Multi(geometry);

-- Recreate the trigger
CREATE TRIGGER refresh_parcel_boundary_geojson_trigger
AFTER INSERT OR UPDATE OF geometry ON parcels
FOR EACH ROW
EXECUTE FUNCTION refresh_parcel_boundary_geojson();
