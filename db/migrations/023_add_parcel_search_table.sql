-- Build a dedicated search projection table so address search can evolve
-- independently of the core parcels table.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS parcel_search (
    parcel_id bigint PRIMARY KEY REFERENCES parcels(id) ON DELETE CASCADE,
    county_id integer NOT NULL REFERENCES counties(id),
    objectid bigint NOT NULL,
    site_address text,
    site_address_norm text,
    mailing_city text,
    mailing_zip5 varchar(5),
    mailing_zip4 varchar(4),
    display_address text,
    lat double precision NOT NULL,
    lng double precision NOT NULL,
    source varchar(32) NOT NULL DEFAULT 'spatial_fallback',
    confidence numeric(3,2) NOT NULL DEFAULT 0.50,
    updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_parcel_search_county_objectid
ON parcel_search (county_id, objectid);

CREATE INDEX IF NOT EXISTS idx_parcel_search_site_norm_prefix
ON parcel_search (lower(site_address_norm) text_pattern_ops)
WHERE site_address_norm IS NOT NULL AND site_address_norm <> '';

CREATE INDEX IF NOT EXISTS idx_parcel_search_site_norm_trgm
ON parcel_search USING gin (lower(site_address_norm) gin_trgm_ops)
WHERE site_address_norm IS NOT NULL AND site_address_norm <> '';

CREATE INDEX IF NOT EXISTS idx_parcel_search_display_prefix
ON parcel_search (lower(display_address) text_pattern_ops)
WHERE display_address IS NOT NULL AND display_address <> '';

CREATE INDEX IF NOT EXISTS idx_parcel_search_display_trgm
ON parcel_search USING gin (lower(display_address) gin_trgm_ops)
WHERE display_address IS NOT NULL AND display_address <> '';

CREATE INDEX IF NOT EXISTS idx_parcel_search_city_prefix
ON parcel_search (lower(mailing_city) text_pattern_ops)
WHERE mailing_city IS NOT NULL AND mailing_city <> '';

CREATE INDEX IF NOT EXISTS idx_parcel_search_city_trgm
ON parcel_search USING gin (lower(mailing_city) gin_trgm_ops)
WHERE mailing_city IS NOT NULL AND mailing_city <> '';

CREATE INDEX IF NOT EXISTS idx_parcel_search_zip5
ON parcel_search (mailing_zip5)
WHERE mailing_zip5 IS NOT NULL AND mailing_zip5 <> '';

-- USPS-style ZIP polygons (ZCTA) imported out-of-band by operator.
CREATE TABLE IF NOT EXISTS us_zip5_areas (
    zip5 varchar(5) PRIMARY KEY,
    geom geometry(MultiPolygon, 4326) NOT NULL,
    updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_us_zip5_areas_geom
ON us_zip5_areas USING gist (geom);

-- ZIP to preferred city mapping imported out-of-band by operator.
CREATE TABLE IF NOT EXISTS us_zip5_city_lookup (
    zip5 varchar(5) NOT NULL,
    city text NOT NULL,
    state varchar(2) NOT NULL DEFAULT 'GA',
    is_preferred boolean NOT NULL DEFAULT false,
    updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (zip5, city, state)
);

CREATE INDEX IF NOT EXISTS idx_us_zip5_city_lookup_zip_state
ON us_zip5_city_lookup (zip5, state, is_preferred DESC, city);
