# Database Design

This document provides detailed documentation of the PostgreSQL/PostGIS database schema used.

---

## Overview

The database uses PostgreSQL 18.1 with the PostGIS extension for spatial data operations. The schema is designed for:

- **Read-heavy workloads**: Precomputed columns eliminate expensive per-request calculations
- **Resumable imports**: Checkpoint tracking allows interrupted imports to continue
- **Flexible data sources**: Field mapping tables handle varying source API schemas
- **Efficient tile serving**: Pre-generated vector tiles with gzip compression

---

## Table of Contents

- [Overview](#overview)
- [Entity Relationship Diagram](#entity-relationship-diagram)
- [Tables](#tables)
    - [`counties`](#counties)
    - [`parcels`](#parcels)
    - [`parcel_taxes`](#parcel_taxes)
    - [`tiles`](#tiles)
    - [`county_field_mappings`](#county_field_mappings)
    - [`parcel_class_codes`](#parcel_class_codes)
    - [`import_checkpoints`](#import_checkpoints)
- [Spatial Considerations](#spatial-considerations)
- [Precomputed Columns](#precomputed-columns)
- [Vector Tile Generation](#vector-tile-generation)
- [Performance Metrics](#performance-metrics)

## Entity Relationship Diagram

```mermaid
erDiagram
    counties ||--o{ parcels : "id <- county_id"
    counties ||--o{ parcel_taxes : "id <- county_id"
    counties ||--o{ county_field_mappings : "id <- county_id"
    counties ||--o{ parcel_class_codes : "id <- county_id"
    parcels ||--o{ parcel_taxes : "id <- parcel_id"

    counties {
        smallint id PK
        varchar(50) name UK
        varchar(2) state
        geometry boundary "MultiPolygon 4326"
        geometry boundary_simplified "MultiPolygon 4326"
        geometry centroid "Point 4326"
        geometry bbox "Polygon 4326"
        jsonb boundary_geojson "Precomputed"
        jsonb boundary_simplified_geojson "Precomputed"
        bigint population
        varchar(50) region
        numeric acres
        numeric square_miles
        text gis_api_url
        text tax_api_url
        varchar(20) tax_provider
        integer max_record_count
        boolean is_government_window
    }

    parcels {
        bigint id PK
        integer county_id FK "-> counties.id"
        varchar(50) parcel_id
        bigint objectid UK
        text site_address
        text site_number
        text owner_name
        text owner_address
        numeric acres
        varchar(255) classification
        varchar(255) tax_district
        geometry geometry "MultiPolygon 3857"
        float4 search_lat
        float4 search_lng
        timestamp last_sync
        boolean processed
        text error_message
        timestamp created_at
        timestamp updated_at
    }

    parcel_taxes {
        bigint id PK
        integer county_id FK "-> counties.id"
        bigint parcel_id FK "-> parcels.id"
        smallint tax_year UK
        numeric tax_amount
        numeric appraised
        numeric assessed
        numeric millage
        text payer_name
        text bill_url
        numeric building_value
        numeric land_value
        varchar(20) due_date
        varchar(20) paid_date
        numeric total_due
        numeric back_taxes
        timestamp last_updated_date
    }

    tiles {
        smallint z PK
        integer x PK
        integer y PK
        varchar(50) layer PK
        bytea data
        timestamp created_at
    }

    county_field_mappings {
        bigint id PK
        integer county_id FK "-> counties.id"
        varchar(255) source_field
        varchar(50) target_column
        text transform
    }

    parcel_class_codes {
        smallint id PK
        integer county_id FK "-> counties.id"
        varchar(10) code UK
        text description
        varchar(20) category
        varchar(7) color
        boolean is_residential "Generated"
    }

    import_checkpoints {
        integer id PK
        varchar(50) county_name UK
        varchar(20) import_type UK
        bigint last_processed_id
        varchar(20) status
        timestamp start_time
        timestamp end_time
        integer total_processed
        integer total_failed
        timestamp created_at
        timestamp updated_at
    }
```

---

## Tables

### `counties`

Stores Georgia county boundaries and associated metadata. This is the foundation table that all parcel data references.

| Column | Type | Description |
|--------|------|-------------|
| `id` | `smallint` | Primary key (1-159 for Georgia counties) |
| `name` | `varchar(50)` | County name (unique) |
| `state` | `varchar(2)` | State abbreviation (default: 'GA') |
| `boundary` | `geometry(MultiPolygon, 4326)` | Full-resolution county boundary |
| `boundary_simplified` | `geometry(MultiPolygon, 4326)` | Simplified boundary for low-zoom rendering |
| `centroid` | `geometry(Point, 4326)` | Geographic center point |
| `bbox` | `geometry(Polygon, 4326)` | Bounding box envelope |
| `boundary_geojson` | `jsonb` | Legacy precomputed GeoJSON blob (not used by current county tile serving path) |
| `boundary_simplified_geojson` | `jsonb` | Legacy precomputed simplified GeoJSON blob |
| `population` | `bigint` | 2010 census population |
| `region` | `varchar(50)` | Georgia regional commission name |
| `acres` | `numeric` | Total land area in acres |
| `square_miles` | `numeric` | Total land area in square miles |
| `gis_api_url` | `text` | ArcGIS REST API endpoint for parcel data |
| `tax_api_url` | `text` | Tax data API endpoint |
| `tax_provider` | `varchar(20)` | Tax data provider (GovWindow, Wildfire, etc.) |
| `max_record_count` | `integer` | API pagination limit (default: 1000) |
| `is_government_window` | `boolean` | Whether county uses Government Window tax platform |

**Indexes:**
- `counties_pkey` - Primary key (btree on `id`)
- `counties_name_key` - Unique constraint (btree on `name`)
- `idx_counties_boundary` - Spatial index (GiST on `boundary`)
- `idx_counties_boundary_simplified` - Spatial index (GiST on `boundary_simplified`)
- `idx_counties_centroid` - Spatial index (GiST on `centroid`)

---

### `parcels`

The main table storing individual property parcels. Contains ~4.77 million records covering all Georgia counties.

| Column | Type | Description |
|--------|------|-------------|
| `id` | `bigint` | Primary key (auto-incrementing) |
| `county_id` | `integer` | Foreign key to `counties.id` |
| `parcel_id` | `varchar(50)` | County-specific parcel identifier |
| `objectid` | `bigint` | Source system object ID (unique per county) |
| `site_address` | `text` | Property street address |
| `site_number` | `text` | Street number (extracted for display) |
| `owner_name` | `text` | Property owner name |
| `owner_address` | `text` | Owner mailing address |
| `acres` | `double precision` | Parcel size in acres |
| `classification` | `varchar(255)` | Land use classification code |
| `tax_district` | `varchar(255)` | Tax jurisdiction district |
| `geometry` | `geometry(MultiPolygon, 3857)` | Parcel boundary in Web Mercator |
| `search_lat` | `real` | Precomputed parcel search latitude (WGS84) |
| `search_lng` | `real` | Precomputed parcel search longitude (WGS84) |
| `last_sync` | `timestamp` | Last successful data sync |
| `processed` | `boolean` | NULL=success, FALSE=needs retry |
| `error_message` | `text` | Error details if processing failed |
| `created_at` | `timestamp` | Record creation time |
| `updated_at` | `timestamp` | Last modification time |

**Indexes:**
- `parcels_pkey` - Primary key (btree on `id`)
- `idx_parcels_county_parcel` - Composite index for lookups (btree on `county_id`, `parcel_id`)
- `idx_parcels_geometry` - Spatial index (GiST on `geometry`)
- `parcels_county_id_objectid_key` - Unique constraint per county (btree on `county_id`, `objectid`)
- `idx_parcels_owner_name_prefix_active` - Partial prefix search index for active owner autocomplete
- `idx_parcels_owner_name_trgm_active` - Partial trigram search index for active owner fuzzy search

> **Note on SRID**: Parcels use SRID 3857 (Web Mercator) because vector tiles are generated in this projection. County boundaries use SRID 4326 (WGS84) and are transformed during MVT generation.

---

### `parcel_taxes`

Historical tax records for parcels. Each parcel can have multiple records (one per tax year).

| Column | Type | Description |
|--------|------|-------------|
| `id` | `bigint` | Primary key |
| `county_id` | `integer` | Foreign key to `counties.id` |
| `parcel_id` | `bigint` | Foreign key to `parcels.id` |
| `tax_year` | `smallint` | Tax year (e.g., 2024) |
| `tax_amount` | `numeric` | Total tax owed |
| `appraised` | `numeric(14,2)` | Fair market value |
| `assessed` | `numeric(14,2)` | Assessed value (40% of appraised in GA) |
| `millage` | `numeric(8,6)` | Tax rate as decimal (e.g., 0.032) |
| `payer_name` | `text` | Name of tax payer |
| `bill_url` | `text` | Direct link to tax bill/details page (when available) |
| `building_value` | `numeric` | Building improvement value |
| `land_value` | `numeric` | Land value |
| `due_date` | `varchar(20)` | Tax due date as provided by source |
| `paid_date` | `varchar(20)` | Date paid as provided by source |
| `total_due` | `numeric` | Total amount currently due |
| `back_taxes` | `numeric` | Prior unpaid tax balance |
| `last_updated_date` | `timestamp` | Last tax portal update timestamp |

**Indexes:**
- `parcel_taxes_pkey` - Primary key
- `idx_tax_year` - Unique constraint (btree on `parcel_id`, `tax_year`)

---

### `tiles`

Pre-generated Mapbox Vector Tiles (MVT) stored as gzipped binary data.

| Column | Type | Description |
|--------|------|-------------|
| `z` | `smallint` | Zoom level (13-19 for parcels) |
| `x` | `integer` | Tile X coordinate |
| `y` | `integer` | Tile Y coordinate |
| `layer` | `varchar(50)` | Layer name (e.g., `parcels`, `counties`, `tax_heatmap_2024`) |
| `data` | `bytea` | Gzipped MVT binary data |
| `created_at` | `timestamp` | Tile generation time |

**Primary Key:** Composite (`z`, `x`, `y`, `layer`)

**Indexes:**
- `tiles_pkey` - Primary key (btree)
- `idx_tiles_lookup` - Query optimization (btree on `z`, `x`, `y`, `layer`)

**Check Constraints:**
- `z` must be between 0 and 30
- `x` and `y` must be non-negative

> **Note:** Tax heatmap grid tiles are precomputed and stored as `tax_heatmap_<year>` layers.  
> Parcel-level tax tiles are precomputed as `tax_parcels_<county_id>_<year>` when generated, and the API can fall back to runtime generation if a tile is missing.

---

### `county_field_mappings`

Maps source API field names to our standardized schema. Different counties expose data with different field names.

| Column | Type | Description |
|--------|------|-------------|
| `id` | `bigint` | Primary key |
| `county_id` | `integer` | Foreign key to `counties.id` |
| `source_field` | `varchar(255)` | Field name in source API |
| `target_column` | `varchar(50)` | Column name in `parcels` table |
| `transform` | `text` | Optional transformation rule |

**Example mappings:**
```
Fulton County:   "OWNER1" → "owner_name"
DeKalb County:   "OwnerName" → "owner_name"
Gwinnett County: "OWNER_NAME" → "owner_name"
```

---

### `parcel_class_codes`

Land use classification codes with category and color for map styling.

| Column | Type | Description |
|--------|------|-------------|
| `id` | `smallint` | Primary key |
| `county_id` | `integer` | Foreign key to `counties.id` |
| `code` | `varchar(10)` | Classification code (e.g., "R1", "C2") |
| `description` | `text` | Human-readable description |
| `category` | `varchar(20)` | Category (Residential, Commercial, etc.) |
| `color` | `varchar(7)` | Hex color for map display |
| `is_residential` | `boolean` | Generated column (true if category = 'Residential') |

---

### `import_checkpoints`

Tracks import progress for resumable batch operations.

| Column | Type | Description |
|--------|------|-------------|
| `id` | `integer` | Primary key |
| `county_name` | `varchar(50)` | County being imported |
| `import_type` | `varchar(20)` | Import workflow key (e.g. `parcel`, `tax`, `parcel_search`, `owner_groups`, `qpublic_enrich`) |
| `last_processed_id` | `bigint` | Last successfully processed record ID |
| `status` | `varchar(20)` | RUNNING, COMPLETED, or FAILED |
| `start_time` | `timestamp` | Import start time |
| `end_time` | `timestamp` | Import completion time |
| `total_processed` | `integer` | Successfully processed count |
| `total_failed` | `integer` | Failed record count |
| `created_at` | `timestamp` | Checkpoint creation time |
| `updated_at` | `timestamp` | Last checkpoint update |

**Unique Constraint:** (`county_name`, `import_type`)

---

## Spatial Considerations

### Coordinate Reference Systems (SRID)

| Data Type | SRID | Name | Reason |
|-----------|------|------|--------|
| County boundaries | 4326 | WGS84 | Standard for GeoJSON, GPS coordinates |
| Parcel geometries | 3857 | Web Mercator | Required for MVT tile generation |

### Geometry Simplification

County boundaries are stored in two resolutions:

```sql
-- Full resolution (for detailed view)
boundary geometry(MultiPolygon, 4326)

-- Simplified for fast rendering at low zoom
boundary_simplified geometry(MultiPolygon, 4326)
-- Generated with: ST_Simplify(boundary, 0.005)
-- Tolerance of 0.005° ≈ 500m at Georgia's latitude
```

### Spatial Indexes

All geometry columns use GiST (Generalized Search Tree) indexes:

```sql
CREATE INDEX idx_counties_boundary ON counties USING gist(boundary);
CREATE INDEX idx_parcels_geometry ON parcels USING gist(geometry);
```

---

## Precomputed Columns

### Active precomputation paths

The current serving path emphasizes tile and lookup precomputation:

- County/parcel/tax layers are pre-generated as MVT and stored gzipped in `tiles`.
- `parcels.search_lat/search_lng` are trigger-maintained for fast map/search responses.
- `parcel_search` is a materialized projection for address autocomplete and display ranking.
- `owner_groups` + `owner_group_members` provide a fast materialized path for reverse-owner lookup.

### Legacy county GeoJSON columns

`counties.boundary_geojson` and `counties.boundary_simplified_geojson` may still exist in older databases, but current map rendering serves county vector tiles (`/api/tiles/counties/...`) rather than GeoJSON payloads.

### Search Precomputation

Parcel search uses precomputed `search_lat`/`search_lng` columns and indexed text matching so autocomplete does not run expensive geometry functions per keystroke.

- `search_lat` / `search_lng` are maintained by trigger when `parcels.geometry` changes.
- Address autocomplete uses the `parcel_search` projection table with prefix + trigram indexes on normalized address fields.
- Owner autocomplete uses partial prefix + trigram indexes on `parcels.owner_name` for active records (`processed IS NULL`, `objectid IS NOT NULL`, non-null coords/text).
- The API endpoint `/api/search/parcels` supports `mode=address|owner` with prefix-first + trigram fallback.

### Reverse Owner Matching

Reverse owner lookup (`/api/owners/properties`) is implemented as a hybrid pipeline:

1. **Materialized lookup path**  
   Uses `owner_group_members` + `owner_groups` (built by `--build-owner-groups`) for fast precomputed group retrieval.

2. **Dynamic scoring fallback**  
   If a materialized group is unavailable/incomplete, candidates are scored at request time using normalized owner-name/address rules.

Confidence scoring combines:

- Strict/relaxed owner-address key matches
- House/street/city/ZIP combinations
- Owner-name exact/subset/overlap similarity
- PO Box ambiguity penalties
- Minor county/proximity tie-breakers

The API returns:

- `match_confidence` (`0..100`)
- `match_band` (`high` >= 85, `medium` >= 55, `low` < 55)

---
## Vector Tile Generation

### Overview

Vector tiles are generated using PostGIS's `ST_AsMVT` function and stored pre-compressed in the `tiles` table.

### Generation Flow

```mermaid
flowchart LR
    A[County Bounding Box] --> B[Calculate tile coordinates<br/>for zoom 13-19]
    B --> C[For each tile Z/X/Y]
    C --> D[Query parcels intersecting<br/>tile envelope]
    D --> E[ST_AsMVTGeom transforms<br/>geometry to tile space]
    E --> F[ST_AsMVT creates<br/>binary MVT]
    F --> G[Gzip compress]
    G --> H[Store in tiles table]
```

### Tile Query Example

```sql
SELECT ST_AsMVT(tile, 'parcels', 4096, 'geom', 'feature_id') AS mvt
FROM (
    SELECT 
        p.id AS feature_id,
        p.parcel_id,
        p.site_address,
        p.owner_name,
        p.acres,
        p.classification,
        ST_AsMVTGeom(
            p.geometry,
            ST_TileEnvelope(z, x, y),
            4096,  -- extent
            256,   -- buffer
            true   -- clip
        ) AS geom
    FROM parcels p
    WHERE ST_Intersects(p.geometry, ST_TileEnvelope(z, x, y))
) AS tile;
```

### Tax Heatmap Tile Strategy

Tax visualization uses two tile paths:

1. **Precomputed grid tiles** (`tax_heatmap_<year>`)  
   Stored in the `tiles` table and served directly for low zoom levels (county overview).

2. **Parcel tax tiles** (`tax_parcels_<county_id>_<year>`)  
   Precomputed and stored in `tiles` for high zoom levels, with runtime generation as fallback when a specific tile is missing.

This split avoids rendering parcel geometry at low zoom while preserving parcel precision at high zoom.


## Performance Metrics

### Query Performance

| Query Type | Cold | Cached | Notes |
|------------|------|--------|-------|
| County tile lookup | 5-10ms | <1ms | `tiles` table read on miss, in-memory county tile cache on hit |
| Single tile lookup | 5-10ms | <1ms | Parcel tile LRU cache on hit |
| Parcel by ID | <1ms | N/A | Primary key lookup |
| Parcels in bbox | 10-50ms | N/A | Depends on result count |
