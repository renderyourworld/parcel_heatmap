// Global variable for the map instance
let map = null;

// Global variables for managing layers
let simplifiedCountyLayer = null;
let fullCountyLayer = null;
let labelLayerGroup = null; // Separate layer for labels (render once)
let simplifiedGeoJSON = null; // Cache for simplified boundaries
let fullGeoJSON = null; // Cache for full boundaries (preloaded in background)

// Constants
const GEORGIA_CENTER = [32.8, -83.6];
const INITIAL_ZOOM = 8;
const DETAIL_ZOOM_THRESHOLD = 11; // Switch to full boundary detail at zoom 11+
const PARCEL_ZOOM_THRESHOLD = 13; // Start loading parcels at zoom 13+
const API_BASE = 'http://localhost:9000/api/counties';

// Debounce utility: delays invoking func until after wait milliseconds
// have elapsed since the last time the debounced function was invoked.
function debounce(func, wait) {
    let timeout;
    return function(...args) {
        const context = this;
        clearTimeout(timeout);
        timeout = setTimeout(() => func.apply(context, args), wait);
    };
}

// Handler for zoom/move events
function onMapZoomOrMove() {
    const currentZoom = map.getZoom();

    // If zoomed to parcel level, delegate to parcel handler
    if (currentZoom >= PARCEL_ZOOM_THRESHOLD) {
        // Hide both county layers at parcel zoom level
        map.removeLayer(simplifiedCountyLayer);
        map.removeLayer(fullCountyLayer);
        
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

    // At zoom 11+: render full boundaries from cache
    renderFullBoundaries(fullGeoJSON);
}

 // Initialize the map and set up event handlers.
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
        
        // Preload full boundaries in the background (do not await; let it load asynchronously)
        // This way, by the time the user zooms to full-detail level, the data is already cached
        preloadFullBoundaries();
    });

    // Debounced handler for detailed boundaries (zoom 11+)
    const debouncedZoomHandler = debounce(onMapZoomOrMove, 300);
    map.on('zoomend', debouncedZoomHandler);
    map.on('moveend', debouncedZoomHandler);
}

// Load and render simplified county boundaries (all counties, cached).
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
}

// Render simplified county boundaries (low zoom levels 8-10).
function renderSimplifiedBoundaries(geoJsonData) {
    // Create the layer once if it doesn't exist
    if (!simplifiedCountyLayer) {
        simplifiedCountyLayer = L.geoJSON(geoJsonData, {
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
        });
    }

    // Show simplified layer, hide full layer
    if (!map.hasLayer(simplifiedCountyLayer)) {
        simplifiedCountyLayer.addTo(map);
    }
    if (fullCountyLayer && map.hasLayer(fullCountyLayer)) {
        map.removeLayer(fullCountyLayer);
    }
}

// Render county name labels once (based on simplified boundary centroids).
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

// Preload full county boundaries in the background (without rendering).
// This ensures the full boundaries are cached before the user zooms in to view them.
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
            fullGeoJSON = data; // Cache for reuse
            console.log('Full county boundaries preloaded and cached (' + JSON.stringify(data).length + ' bytes)');
        })
        .catch(error => {
            console.error('Error preloading full county boundaries:', error);
        });
}

// Render full county boundaries (high zoom levels 11-12).
function renderFullBoundaries(geoJsonData) {
    // Create the layer once if it doesn't exist
    if (!fullCountyLayer) {
        fullCountyLayer = L.geoJSON(geoJsonData, {
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
        });
    }

    // Show full layer, hide simplified layer
    if (!map.hasLayer(fullCountyLayer)) {
        fullCountyLayer.addTo(map);
    }
    if (simplifiedCountyLayer && map.hasLayer(simplifiedCountyLayer)) {
        map.removeLayer(simplifiedCountyLayer);
    }
}

// Build rich HTML popup content from county properties.
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

// Initialize the map when the page loads
initMap();