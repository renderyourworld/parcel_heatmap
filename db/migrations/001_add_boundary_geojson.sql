-- Add precomputed GeoJSON columns for boundary and simplified boundary
-- These are maintained by triggers to avoid expensive ST_AsGeoJSON calls per-request

ALTER TABLE counties ADD COLUMN IF NOT EXISTS boundary_geojson jsonb;
ALTER TABLE counties ADD COLUMN IF NOT EXISTS boundary_simplified_geojson jsonb;

-- Populate boundary_geojson with precomputed Feature JSON
UPDATE counties
SET boundary_geojson = jsonb_build_object(
    'type', 'Feature',
    'geometry', ST_AsGeoJSON(boundary, 6)::jsonb,
    'properties', jsonb_build_object(
        'id', id,
        'name', name,
        'state', state,
        'population', population,
        'region', region,
        'acres', acres,
        'square_miles', square_miles,
        'gis_api_url', gis_api_url,
        'tax_api_url', tax_api_url,
        'centroid', ST_AsGeoJSON(centroid)::jsonb
    )
)
WHERE boundary_geojson IS NULL;

-- Populate boundary_simplified_geojson with precomputed simplified Feature JSON
UPDATE counties
SET boundary_simplified_geojson = jsonb_build_object(
    'type', 'Feature',
    'geometry', ST_AsGeoJSON(boundary_simplified, 6)::jsonb,
    'properties', jsonb_build_object(
        'id', id,
        'name', name,
        'state', state,
        'population', population,
        'region', region,
        'acres', acres,
        'square_miles', square_miles,
        'gis_api_url', gis_api_url,
        'tax_api_url', tax_api_url,
        'centroid', ST_AsGeoJSON(centroid)::jsonb
    )
)
WHERE boundary_simplified_geojson IS NULL;

-- Create or replace trigger function to refresh both GeoJSON columns on geometry changes
CREATE OR REPLACE FUNCTION refresh_boundary_geojson()
RETURNS TRIGGER AS $$
BEGIN
    -- Refresh full boundary GeoJSON
    IF NEW.boundary IS DISTINCT FROM OLD.boundary OR NEW.centroid IS DISTINCT FROM OLD.centroid THEN
        NEW.boundary_geojson := jsonb_build_object(
            'type', 'Feature',
            'geometry', ST_AsGeoJSON(NEW.boundary, 6)::jsonb,
            'properties', jsonb_build_object(
                'id', NEW.id,
                'name', NEW.name,
                'state', NEW.state,
                'population', NEW.population,
                'region', NEW.region,
                'acres', NEW.acres,
                'square_miles', NEW.square_miles,
                'gis_api_url', NEW.gis_api_url,
                'tax_api_url', NEW.tax_api_url,
                'centroid', ST_AsGeoJSON(NEW.centroid)::jsonb
            )
        );
    END IF;

    -- Refresh simplified boundary GeoJSON
    IF NEW.boundary_simplified IS DISTINCT FROM OLD.boundary_simplified OR NEW.centroid IS DISTINCT FROM OLD.centroid THEN
        NEW.boundary_simplified_geojson := jsonb_build_object(
            'type', 'Feature',
            'geometry', ST_AsGeoJSON(NEW.boundary_simplified, 6)::jsonb,
            'properties', jsonb_build_object(
                'id', NEW.id,
                'name', NEW.name,
                'state', NEW.state,
                'population', NEW.population,
                'region', NEW.region,
                'acres', NEW.acres,
                'square_miles', NEW.square_miles,
                'gis_api_url', NEW.gis_api_url,
                'tax_api_url', NEW.tax_api_url,
                'centroid', ST_AsGeoJSON(NEW.centroid)::jsonb
            )
        );
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Drop old trigger if it exists and create new one
DROP TRIGGER IF EXISTS refresh_boundary_geojson_trigger ON counties;
CREATE TRIGGER refresh_boundary_geojson_trigger
BEFORE INSERT OR UPDATE ON counties
FOR EACH ROW
EXECUTE FUNCTION refresh_boundary_geojson();
