// Global variable for the map instance
let map = null;

// Global variables for managing layers
let simplifiedCountyLayer = null;
let fullCountyLayer = null;
let labelLayerGroup = null; // Separate layer for county labels (render once)
let parcelLayer = null; // Layer for parcel boundaries (zoom 16+)
let parcelLabelLayerGroup = null; // Separate layer for parcel site number labels (zoom 17+)
let simplifiedGeoJSON = null; // Cache for simplified boundaries
let fullGeoJSON = null; // Cache for full boundaries (preloaded in background)
let selectedParcel = null; // Track currently selected parcel for highlight

// Parcel caching - track loaded tiles using grid system
let loadedTiles = new Set(); // Tracks tile keys like "123_456"
let parcelFetchInProgress = false;
const TILE_GRID_SIZE = 0.01; // ~1.1km at Georgia's latitude

// Constants
const GEORGIA_CENTER = [32.8, -83.6];
const INITIAL_ZOOM = 8;
const DETAIL_ZOOM_THRESHOLD = 11; // Switch to full boundary detail at zoom 11+
const PARCEL_ZOOM_THRESHOLD = 16; // Start loading parcels at zoom 16+
const PARCEL_LABEL_ZOOM_THRESHOLD = 17; // Show parcel site number labels at zoom 17+
const API_BASE = 'http://localhost:9000/api/counties';
const PARCEL_API_BASE = 'http://localhost:9000/api/parcels';

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

    // If zoomed to parcel level, load and render parcels
    if (currentZoom >= PARCEL_ZOOM_THRESHOLD) {
        // Hide both county layers at parcel zoom level
        if (simplifiedCountyLayer && map.hasLayer(simplifiedCountyLayer)) {
            map.removeLayer(simplifiedCountyLayer);
        }
        if (fullCountyLayer && map.hasLayer(fullCountyLayer)) {
            map.removeLayer(fullCountyLayer);
        }
        
        // Show/hide labels based on zoom level
        if (currentZoom >= PARCEL_LABEL_ZOOM_THRESHOLD) {
            // At zoom 17+, show labels if they exist
            if (parcelLabelLayerGroup && !map.hasLayer(parcelLabelLayerGroup)) {
                parcelLabelLayerGroup.addTo(map);
            }
        } else {
            // Below zoom 17, hide labels
            if (parcelLabelLayerGroup && map.hasLayer(parcelLabelLayerGroup)) {
                map.removeLayer(parcelLabelLayerGroup);
            }
        }
        
        // Load parcels for current viewport
        fetchAndRenderParcels();
        return;
    }

    // Clear parcel layer, labels, and cache when zooming out below threshold
    if (parcelLayer && map.hasLayer(parcelLayer)) {
        map.removeLayer(parcelLayer);
        parcelLayer.clearLayers();
        parcelLayer = null;
    }
    if (parcelLabelLayerGroup && map.hasLayer(parcelLabelLayerGroup)) {
        map.removeLayer(parcelLabelLayerGroup);
        parcelLabelLayerGroup.clearLayers();
        parcelLabelLayerGroup = null;
    }
    // Clear tile cache when zooming out
    loadedTiles.clear();

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

    // Create label layer groups (separate from boundaries)
    labelLayerGroup = L.layerGroup().addTo(map);
    parcelLabelLayerGroup = L.layerGroup(); // Don't add to map yet - only at parcel zoom

    // Add zoom level indicator for debugging
    const zoomIndicator = L.control({ position: 'bottomright' });
    zoomIndicator.onAdd = function() {
        const div = L.DomUtil.create('div', 'zoom-indicator');
        div.style.background = 'rgba(255, 255, 255, 0.9)';
        div.style.padding = '8px 12px';
        div.style.border = '2px solid #333';
        div.style.borderRadius = '4px';
        div.style.fontWeight = 'bold';
        div.style.fontSize = '14px';
        div.innerHTML = 'Zoom: ' + map.getZoom();
        return div;
    };
    zoomIndicator.addTo(map);

    // Update zoom indicator on zoom
    map.on('zoomend', function() {
        document.querySelector('.zoom-indicator').innerHTML = 'Zoom: ' + map.getZoom();
    });

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

// Calculate which grid tiles the viewport covers
function getBboxTiles(bounds) {
    const minTileX = Math.floor(bounds.getWest() / TILE_GRID_SIZE);
    const maxTileX = Math.floor(bounds.getEast() / TILE_GRID_SIZE);
    const minTileY = Math.floor(bounds.getSouth() / TILE_GRID_SIZE);
    const maxTileY = Math.floor(bounds.getNorth() / TILE_GRID_SIZE);
    
    const tiles = [];
    for (let x = minTileX; x <= maxTileX; x++) {
        for (let y = minTileY; y <= maxTileY; y++) {
            tiles.push({ x, y, key: `${x}_${y}` });
        }
    }
    return tiles;
}

// Get the minimal bbox covering a set of tiles
function getTilesBbox(tiles) {
    const minX = Math.min(...tiles.map(t => t.x)) * TILE_GRID_SIZE;
    const maxX = (Math.max(...tiles.map(t => t.x)) + 1) * TILE_GRID_SIZE;
    const minY = Math.min(...tiles.map(t => t.y)) * TILE_GRID_SIZE;
    const maxY = (Math.max(...tiles.map(t => t.y)) + 1) * TILE_GRID_SIZE;
    return { minX, maxX, minY, maxY };
}

// Fetch and render parcels for the current map viewport (zoom 16+)
// Uses tile-based grid caching - only fetches missing tiles
function fetchAndRenderParcels() {
    const bounds = map.getBounds();
    const neededTiles = getBboxTiles(bounds);
    
    // Filter to only tiles we haven't loaded yet
    const missingTiles = neededTiles.filter(t => !loadedTiles.has(t.key));
    
    if (missingTiles.length === 0) {
        console.log(`Using cached parcels (all ${neededTiles.length} tiles loaded)`);
        return;
    }

    // Prevent duplicate fetches
    if (parcelFetchInProgress) {
        console.log('Parcel fetch already in progress, skipping...');
        return;
    }

    // Calculate the minimal bbox covering all missing tiles
    const bbox = getTilesBbox(missingTiles);
    const url = `${PARCEL_API_BASE}?minX=${bbox.minX}&minY=${bbox.minY}&maxX=${bbox.maxX}&maxY=${bbox.maxY}`;

    console.log(`Fetching ${missingTiles.length} new tiles (${loadedTiles.size} cached):`, bbox);
    parcelFetchInProgress = true;

    fetch(url)
        .then(response => {
            if (!response.ok) {
                throw new Error('Failed to fetch parcels');
            }
            return response.json();
        })
        .then(data => {
            // Create parcel layer with CANVAS renderer for better performance
            if (!parcelLayer) {
                parcelLayer = L.layerGroup().addTo(map);
            }

            // ADD new parcels to existing layer using canvas renderer
            const newParcelLayer = L.geoJSON(data, {
                renderer: L.canvas(), // Use canvas for fast rendering of many features
                style: {
                    color: '#333',
                    weight: 1,
                    fillOpacity: 0.1,
                    fillColor: '#4A90E2'
                },
                onEachFeature: function(feature, layer) {
                    if (feature.properties) {
                        layer.bindPopup(buildParcelPopupHTML(feature.properties));
                        
                        // Add click handler for highlighting
                        layer.on('click', function() {
                            // Reset previously selected parcel
                            if (selectedParcel) {
                                selectedParcel.setStyle({
                                    color: '#333',
                                    weight: 1
                                });
                            }
                            
                            // Highlight the clicked parcel
                            layer.setStyle({
                                color: '#FF6B35',
                                weight: 3
                            });
                            
                            selectedParcel = layer;
                        });
                    }
                }
            });

            // Add each feature to the persistent parcelLayer
            newParcelLayer.eachLayer(layer => {
                parcelLayer.addLayer(layer);
            });
            
            // Render parcel labels (additive)
            renderParcelLabels(data);
            
            // Mark tiles as loaded
            missingTiles.forEach(t => loadedTiles.add(t.key));
            
            const featureCount = data.features ? data.features.length : 0;
            console.log(`Added ${featureCount} parcels (${loadedTiles.size} tiles cached)`);
        })
        .catch(error => {
            console.error('Error loading parcels:', error);
        })
        .finally(() => {
            parcelFetchInProgress = false;
        });
}

// Render parcel site number labels at parcel zoom level
// Additive approach - adds new labels without clearing existing ones
function renderParcelLabels(geoJsonData) {
    // Create label layer if it doesn't exist
    if (!parcelLabelLayerGroup) {
        parcelLabelLayerGroup = L.layerGroup();
        // Only add to map if we're at zoom 17+
        if (map.getZoom() >= PARCEL_LABEL_ZOOM_THRESHOLD) {
            parcelLabelLayerGroup.addTo(map);
        }
    }

    geoJsonData.features.forEach(feature => {
        const props = feature.properties;
        const siteNumber = props.site_number;

        // Only render label if site_number exists and centroid is available
        if (siteNumber && props.centroid && props.centroid.coordinates) {
            const coords = props.centroid.coordinates;
            const latlng = L.latLng(coords[1], coords[0]); // GeoJSON is [lon, lat]

            // Create a label marker for site number
            L.marker(latlng, {
                icon: L.divIcon({
                    className: 'parcel-label',
                    html: `<span style="font-size: 10px; color: #333; white-space: nowrap;">${siteNumber}</span>`,
                    iconSize: null // No fixed size; auto-size to content
                })
            }).addTo(parcelLabelLayerGroup);
        }
    });
}

// Build popup HTML for parcel properties
function buildParcelPopupHTML(props) {
    const format = (val) => val ?? 'N/A';
    return `
        <div style="font-family: Arial, sans-serif; font-size: 11px; max-width: 200px;">
            <h4 style="margin: 0 0 8px 0; font-size: 13px;">Parcel ${props.parcel_id || 'Unknown'}</h4>
            <hr style="margin: 4px 0;">
            <p style="margin: 4px 0;"><b>Owner:</b> ${format(props.owner_name)}</p>
            <p style="margin: 4px 0;"><b>Address:</b> ${format(props.site_address)}</p>
            <p style="margin: 4px 0;"><b>Acres:</b> ${format(props.acres)}</p>
            <p style="margin: 4px 0;"><b>Classification:</b> ${format(props.classification)}</p>
        </div>
    `;
}

// Initialize the map when the page loads
initMap();