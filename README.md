# Parcel Heatmap

A web application for visualizing Georgia county boundaries and parcel tax data with interactive maps, optimized database queries, and client-side caching.

## Features

- **Interactive County Map**: Zoom-based detail switching with simplified geometry at low zoom, full detail at higher zoom
- **Performance Optimized**: 
  - Precomputed GeoJSON columns avoid expensive per-query geometry serialization
  - Server-side in-memory gzipped response caching
  - Background data preloading for seamless user experience
  - Sub-20ms simplified boundary queries, 2ms cached full-detail queries
- **Rich County Data**: Population, region, acreage, and clickable popups
- **Smart Caching**: Simplified geometry cached client-side, full boundaries preloaded in background
- **Responsive Frontend**: Built with Leaflet.js and OpenStreetMap tiles
- **Production-Ready**: Error handling, logging, retry logic with exponential backoff

## Architecture

### Backend (Go)
- **Framework**: Gin (HTTP router)
- **Database**: PostgreSQL + PostGIS
- **Handler-based organization**:
  - `handlers/county.go`: County boundary endpoint with cache checking
  - `handlers/cache.go`: Thread-safe in-memory gzip cache
  - `handlers/parcel_handler.go`: Bbox-based parcel queries
- **Importers**:
  - `importers/ga_counties.go`: Fetches county boundaries from SAGIS API with retry/backoff

### Database (PostgreSQL/PostGIS)
- **Schema**: Counties and parcels with spatial geometry columns
- **Optimizations**:
  - Precomputed `boundary_geojson` and `boundary_simplified_geojson` JSONB columns
  - Database triggers refresh precomputed columns on geometry updates
  - ST_Simplify with 0.005° tolerance for efficient pan performance

### Frontend (JavaScript)
- **Framework**: Leaflet.js for interactive maps
- **Data Flow**:
```
┌─────────────────────────────────────────────────────────────┐
│ User opens browser                                           │
└─────────────────┬───────────────────────────────────────────┘
                  │
        ┌─────────▼──────────┐
        │ Fetch simplified   │──────► database ──► ST_Simplify() ──┐
        │ boundaries         │                                      │
        └─────────┬──────────┘                                      │
                  │                                                │
        ┌─────────▼──────────┐      ┌──────────────────────────────┘
        │ Render map with    │      │
        │ simplified data    │      ▼
        └─────────┬──────────┘    Precomputed
                  │                JSONB column
        ┌─────────▼──────────┐    (15-20ms)
        │ User sees map      │
        │ immediately!       │
        └────────────────────┘

              (Meanwhile in background...)
        ┌──────────────────────────┐
        │ Fetch full boundaries    │──────► database
        │ asynchronously           │
        │                          │      Precomputed
        │ Gzip + cache response    │    JSONB column
        │ in server memory         │    (120ms first,
        └──────────────────────────┘     2ms cached)

              (User zooms in...)
        ┌──────────────────────────┐
        │ Full boundaries available│
        │ instantly from cache!    │
        │ (2ms response)           │
        └──────────────────────────┘
```
- **Optimization**: Client-side GeoJSON caching, label layer separation

## Performance

| Metric | Value | Notes |
|--------|-------|-------|
| Simplified Query | 15-20ms | Precomputed JSONB column |
| Full Query (cached) | ~2ms | Served from memory |
| Full Query (cold) | ~120ms | First request, then cached |
| Response Size | ~2.75 MB gzipped | All 159 counties |
| Page Load | ~35ms | Simplified map + labels |


## Future Improvements

- Parcel layer rendering (zoom ≥ 13)
- Property Tax history view
- See comparable parcels in other counties
- See all owned properties by owner
- Redis-backed caching for multi-instance deployments
- Unit tests
- Natural Language search support

## Acknowledgments

- Georgia SAGIS API for county data
- OpenStreetMap tiles
- PostGIS spatial database
- Leaflet.js mapping library
