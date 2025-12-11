// Global variable for the map instance
let map = null;

// Global variables for managing layers
let countyBoundaryLayer = null;
let labelLayerGroup = null; // Separate layer for labels (render once)
let simplifiedGeoJSON = null; // Cache for simplified boundaries

// Constants
const GEORGIA_CENTER = [32.8, -83.6];
const INITIAL_ZOOM = 8;
const DETAIL_ZOOM_THRESHOLD = 11; // Switch to full boundary detail at zoom 11+
const PARCEL_ZOOM_THRESHOLD = 13; // Start loading parcels at zoom 13+
const API_BASE = 'http://localhost:9000/api/counties';

/**
 * Debounce utility: delays invoking func until after wait milliseconds
 * have elapsed since the last time the debounced function was invoked.
 */
function debounce(func, wait) {
    let timeout;
    return function(...args) {
        const context = this;
        clearTimeout(timeout);
        timeout = setTimeout(() => func.apply(context, args), wait);
    };
}

/**
 * Initialize the map and set up event handlers.
 */
function initMap() {
    // Create map instance
    map = L.map('map').setView(GEORGIA_CENTER, INITIAL_ZOOM);

    // Add base tile layer
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
        maxZoom: 19,
        attribution: '© OpenStreetMap contributors'
    }).addTo(map);

    // Create label layer group (separate from county boundaries)
    labelLayerGroup = L.layerGroup().addTo(map);

    // Load simplified boundaries on initial page load (no re-fetch on pan/zoom at zoom < 11)
    map.whenReady(function() {
        loadAndRenderSimplifiedBoundaries();
    });

    // Debounced handler for detailed boundaries (zoom 11+)
    const debouncedZoomHandler = debounce(onMapZoomOrMove, 300);
    map.on('zoomend', debouncedZoomHandler);
    map.on('moveend', debouncedZoomHandler);
}


/**
 * Load and render simplified county boundaries (all counties, cached).
 * Also preload full boundaries in the background.
 * Called once on page load.
 */
function loadAndRenderSimplifiedBoundaries() {
    fetch(`${API_BASE}?detail=simplified`)
        .then(response => {
            if (!response.ok) {
                throw new Error('Failed to fetch simplified boundaries');
            }
            return response.json();
        })
        .then(data => {
            simplifiedGeoJSON = data; // Cache for reuse
            renderSimplifiedBoundaries(data);
            renderLabels(data); // Render labels once
        })
        .catch(error => {
            console.error('Error loading simplified county boundaries:', error);
        });

    // Preload full boundaries in the background (do not await; let it load asynchronously)
    // This way, by the time the user zooms to full-detail level, the data is already cached
    preloadFullBoundaries();
}

/**
 * Preload full county boundaries in the background (without rendering).
 * This ensures the full boundaries are cached before the user zooms in to view them.
 */
function preloadFullBoundaries() {
    console.log('Preloading full county boundaries in background...');
    fetch(`${API_BASE}?detail=full`)
        .then(response => {
            if (!response.ok) {
                throw new Error('Failed to preload full boundaries');
            }
            return response.json();
        })
        .then(data => {
            console.log('Full county boundaries preloaded and cached (' + JSON.stringify(data).length + ' bytes)');
        })
        .catch(error => {
            console.error('Error preloading full county boundaries:', error);
        });
}

/**
 * Build rich HTML popup content from county properties.
 */
function buildCountyPopupHTML(props) {
    const format = (val) => val ?? 'N/A';
    return `
        <div style="font-family: Arial, sans-serif; font-size: 12px; max-width: 200px;">
            <h4 style="margin: 0 0 8px 0; font-size: 14px;">${props.name} County, ${props.state || 'GA'}</h4>
            <hr style="margin: 4px 0;">
            <p style="margin: 4px 0;"><b>Population:</b> ${format(props.population)}</p>
            <p style="margin: 4px 0;"><b>Region:</b> ${format(props.region)}</p>
            <p style="margin: 4px 0;"><b>Acres:</b> ${format(props.acres)}</p>
            <p style="margin: 4px 0;"><b>Sq. Miles:</b> ${format(props.square_miles)}</p>
        </div>
    `;
}

/**
 * Render simplified county boundaries (low zoom levels 7-10).
 */
function renderSimplifiedBoundaries(geoJsonData) {
    // Remove old layer if it exists
    if (countyBoundaryLayer) {
        map.removeLayer(countyBoundaryLayer);
    }

    countyBoundaryLayer = L.geoJSON(geoJsonData, {
        style: {
            color: '#007BFF',
            weight: 1,
            fillColor: '#88C0D0',
            fillOpacity: 0.5
        },
        onEachFeature: function(feature, layer) {
            const props = feature.properties;
            layer.bindPopup(buildCountyPopupHTML(props));
        }
    }).addTo(map);
}

/**
 * Render county name labels once (based on simplified boundaries).
 * This layer persists across all zoom levels for visual reference.
 */
function renderLabels(geoJsonData) {
    // Clear old labels if any
    labelLayerGroup.clearLayers();

    geoJsonData.features.forEach(feature => {
        const props = feature.properties;
        const name = props.name || 'Unknown';

        // Use the centroid from the database properties
        if (props.centroid && props.centroid.coordinates) {
            const coords = props.centroid.coordinates;
            const latlng = L.latLng(coords[1], coords[0]); // GeoJSON is [lon, lat]

            // Create a label marker
            L.marker(latlng, {
                icon: L.divIcon({
                    className: 'county-label',
                    html: `<span style="font-size: 12px; font-weight: bold; color: #333; white-space: nowrap;">${name}</span>`,
                    iconSize: null // No fixed size; auto-size to content
                })
            }).addTo(labelLayerGroup);
        }
    });
}

/**
 * Handler for zoom/move events at higher zoom levels (zoom 11+).
 * Loads full boundaries for visible area when zoomed in.
 */
function onMapZoomOrMove() {
    const currentZoom = map.getZoom();

    // If zoomed to parcel level, delegate to parcel handler
    if (currentZoom >= PARCEL_ZOOM_THRESHOLD) {
        if (countyBoundaryLayer) {
            map.removeLayer(countyBoundaryLayer);
            countyBoundaryLayer = null;
        }
        // TODO: Load parcels
        // fetchAndRenderParcels();
        return;
    }

    // If below detail threshold, restore cached simplified boundaries
    if (currentZoom < DETAIL_ZOOM_THRESHOLD) {
        // Always re-render simplified when zooming back out
        renderSimplifiedBoundaries(simplifiedGeoJSON);
        return;
    }

    // At zoom 11+: load full boundaries for visible area
    loadFullBoundariesForVisibleArea();
}

/**
 * Load and render full county boundaries for the visible map area (zoom 11+).
 */
function loadFullBoundariesForVisibleArea() {
    // Request all full county boundaries (endpoint ignores bbox and returns all 159 counties cached)
    fetch(`${API_BASE}?detail=full`)
        .then(response => {
            if (!response.ok) {
                throw new Error('Failed to fetch full boundaries');
            }
            return response.json();
        })
        .then(data => {
            renderFullBoundaries(data);
        })
        .catch(error => {
            console.error('Error loading full county boundaries:', error);
        });
}

/**
 * Render full county boundaries (high zoom levels 11-12).
 */
function renderFullBoundaries(geoJsonData) {
    // Remove old layer if it exists
    if (countyBoundaryLayer) {
        map.removeLayer(countyBoundaryLayer);
    }

    countyBoundaryLayer = L.geoJSON(geoJsonData, {
        style: {
            color: '#007BFF',
            weight: 2,
            fillColor: '#88C0D0',
            fillOpacity: 0.2
        },
        onEachFeature: function(feature, layer) {
            const props = feature.properties;
            layer.bindPopup(buildCountyPopupHTML(props));
        }
    }).addTo(map);
}

// Initialize the map when the page loads
initMap();