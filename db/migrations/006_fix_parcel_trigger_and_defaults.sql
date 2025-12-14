-- Fix parcel trigger to populate centroid and ensure boundary_geojson works
-- Also fix processed column to be nullable (NULL = success, FALSE = error)

-- Make processed column nullable with NULL as default (space efficient)
-- NULL = successfully processed
-- FALSE = error occurred, needs retry
ALTER TABLE parcels ALTER COLUMN processed DROP NOT NULL;
ALTER TABLE parcels ALTER COLUMN processed SET DEFAULT NULL;

-- Ensure created_at and updated_at have proper defaults
ALTER TABLE parcels ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE parcels ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;

-- Drop and recreate trigger function with centroid support
DROP TRIGGER IF EXISTS refresh_parcel_boundary_geojson_trigger ON parcels;

CREATE OR REPLACE FUNCTION refresh_parcel_boundary_geojson()
RETURNS TRIGGER AS $$
BEGIN
    -- Auto-populate centroid from geometry if not set or geometry changed
    IF NEW.geometry IS NOT NULL AND (NEW.centroid IS NULL OR NEW.geometry IS DISTINCT FROM OLD.geometry) THEN
        NEW.centroid := ST_Centroid(NEW.geometry);
    END IF;

    -- Generate GeoJSON Feature with centroid in properties
    IF NEW.geometry IS NOT NULL THEN
        NEW.boundary_geojson := jsonb_build_object(
            'type', 'Feature',
            'geometry', ST_AsGeoJSON(NEW.geometry, 6)::jsonb,
            'properties', jsonb_build_object(
                'parcel_id', NEW.parcel_id,
                'site_address', NEW.site_address,
                'site_number', NEW.site_number,
                'owner_name', NEW.owner_name,
                'owner_address', NEW.owner_address,
                'acres', NEW.acres,
                'classification', NEW.classification,
                'tax_district', NEW.tax_district,
                'centroid', CASE 
                    WHEN NEW.centroid IS NOT NULL 
                    THEN ST_AsGeoJSON(NEW.centroid, 6)::jsonb 
                    ELSE NULL 
                END
            )
        );
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Recreate trigger
CREATE TRIGGER refresh_parcel_boundary_geojson_trigger
BEFORE INSERT OR UPDATE OF geometry ON parcels
FOR EACH ROW
EXECUTE FUNCTION refresh_parcel_boundary_geojson();
