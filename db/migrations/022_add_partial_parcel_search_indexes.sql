-- Align search indexes with live autocomplete filters for better planner use.
-- Address mode
CREATE INDEX IF NOT EXISTS idx_parcels_site_address_prefix_active
ON parcels (lower(site_address) text_pattern_ops)
WHERE processed IS NULL
  AND objectid IS NOT NULL
  AND search_lat IS NOT NULL
  AND search_lng IS NOT NULL
  AND site_address IS NOT NULL
  AND site_address <> '';

CREATE INDEX IF NOT EXISTS idx_parcels_site_address_trgm_active
ON parcels USING gin (lower(site_address) gin_trgm_ops)
WHERE processed IS NULL
  AND objectid IS NOT NULL
  AND search_lat IS NOT NULL
  AND search_lng IS NOT NULL
  AND site_address IS NOT NULL
  AND site_address <> '';

-- Owner mode
CREATE INDEX IF NOT EXISTS idx_parcels_owner_name_prefix_active
ON parcels (lower(owner_name) text_pattern_ops)
WHERE processed IS NULL
  AND objectid IS NOT NULL
  AND search_lat IS NOT NULL
  AND search_lng IS NOT NULL
  AND owner_name IS NOT NULL
  AND owner_name <> '';

CREATE INDEX IF NOT EXISTS idx_parcels_owner_name_trgm_active
ON parcels USING gin (lower(owner_name) gin_trgm_ops)
WHERE processed IS NULL
  AND objectid IS NOT NULL
  AND search_lat IS NOT NULL
  AND search_lng IS NOT NULL
  AND owner_name IS NOT NULL
  AND owner_name <> '';
