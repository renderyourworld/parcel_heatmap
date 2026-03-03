-- Materialized owner grouping tables for fast reverse-owner lookups.
-- Built by importer command: --build-owner-groups

CREATE TABLE IF NOT EXISTS owner_groups (
    id bigserial PRIMARY KEY,
    group_key text NOT NULL UNIQUE,
    key_type varchar(16) NOT NULL,
    canonical_owner_name text,
    canonical_owner_address text,
    is_po_box boolean NOT NULL DEFAULT false,
    member_count integer NOT NULL DEFAULT 0,
    updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_owner_groups_key_type
ON owner_groups (key_type);

CREATE TABLE IF NOT EXISTS owner_group_members (
    parcel_id bigint PRIMARY KEY REFERENCES parcels(id) ON DELETE CASCADE,
    group_id bigint NOT NULL REFERENCES owner_groups(id) ON DELETE CASCADE,
    match_confidence smallint NOT NULL DEFAULT 0,
    match_band varchar(10) NOT NULL DEFAULT 'low',
    updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_owner_group_members_group
ON owner_group_members (group_id);

CREATE INDEX IF NOT EXISTS idx_owner_group_members_confidence
ON owner_group_members (group_id, match_confidence DESC);
