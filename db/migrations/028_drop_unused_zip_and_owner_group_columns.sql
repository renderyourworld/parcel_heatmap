ALTER TABLE us_zip5_areas
DROP COLUMN IF EXISTS source;

ALTER TABLE us_zip5_city_lookup
DROP COLUMN IF EXISTS source;

ALTER TABLE owner_group_members
DROP COLUMN IF EXISTS match_reasons;
