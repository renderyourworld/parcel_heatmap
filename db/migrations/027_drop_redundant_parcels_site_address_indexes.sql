-- Address autocomplete now reads from parcel_search, not parcels.
-- Drop legacy and partial parcels.site_address indexes to reduce index storage.

DROP INDEX IF EXISTS idx_parcels_site_address_prefix;
DROP INDEX IF EXISTS idx_parcels_site_address_trgm;
DROP INDEX IF EXISTS idx_parcels_site_address_prefix_active;
DROP INDEX IF EXISTS idx_parcels_site_address_trgm_active;
