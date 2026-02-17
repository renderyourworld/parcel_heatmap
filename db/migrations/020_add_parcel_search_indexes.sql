-- Add parcel address search acceleration indexes
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Fast prefix autocomplete: lower(site_address) LIKE 'query%'
CREATE INDEX IF NOT EXISTS idx_parcels_site_address_prefix
ON parcels (lower(site_address) text_pattern_ops)
WHERE site_address IS NOT NULL;

-- Fuzzy fallback search: similarity(lower(site_address), lower(query))
CREATE INDEX IF NOT EXISTS idx_parcels_site_address_trgm
ON parcels USING gin (lower(site_address) gin_trgm_ops)
WHERE site_address IS NOT NULL;
