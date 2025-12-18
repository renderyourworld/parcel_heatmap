// MapLibre Vector Tiles
const GEORGIA_CENTER = [-83.6, 32.8]; // [lng, lat]
const INITIAL_ZOOM = 7;

// Initialize the map
const map = new maplibregl.Map({
    container: 'map',
    maxZoom: 19,
    style: {
        version: 8,
        glyphs: 'https://demotiles.maplibre.org/font/{fontstack}/{range}.pbf',
        sources: {
            'osm': {
                type: 'raster',
                tiles: ['https://a.tile.openstreetmap.org/{z}/{x}/{y}.png'],
                tileSize: 256,
                attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
            },
            'parcels': {
                type: 'vector',
                tiles: ['http://localhost:9000/api/tiles/{z}/{x}/{y}'],
                minzoom: 13,
                maxzoom: 19,
                promoteId: 'feature_id'
            }
        },
        layers: [
            {
                id: 'osm-tiles',
                type: 'raster',
                source: 'osm',
                minzoom: 0,
                maxzoom: 22
            },
            {
                id: 'parcel-fill',
                type: 'fill',
                source: 'parcels',
                'source-layer': 'parcels',
                minzoom: 13,
                paint: {
                    'fill-color': [
                        'case',
                        ['boolean', ['feature-state', 'selected'], false],
                        '#ff6b6b',
                        '#3388ff'
                    ],
                    'fill-opacity': [
                        'case',
                        ['boolean', ['feature-state', 'selected'], false],
                        0.6,
                        0.2
                    ]
                }
            },
            {
                id: 'parcel-outline',
                type: 'line',
                source: 'parcels',
                'source-layer': 'parcels',
                minzoom: 13,
                paint: {
                    'line-color': [
                        'case',
                        ['boolean', ['feature-state', 'selected'], false],
                        '#ff0000',
                        '#3388ff'
                    ],
                    'line-width': [
                        'case',
                        ['boolean', ['feature-state', 'selected'], false],
                        3,
                        1
                    ]
                }
            },
            {
                id: 'parcel-labels',
                type: 'symbol',
                source: 'parcels',
                'source-layer': 'parcels',
                minzoom: 15,
                layout: {
                    'text-field': ['get', 'site_number'],
                    'text-size': 10,
                    'text-font': ['Noto Sans Regular'],
                    'text-anchor': 'center',
                    'symbol-avoid-edges': true,
                    'symbol-z-order': 'auto',
                    'symbol-placement': 'point',
                    'text-allow-overlap': false,
                    'text-ignore-placement': false,
                    'text-optional': true
                },
                paint: {
                    'text-color': '#333333',
                    'text-halo-color': '#ffffff',
                    'text-halo-width': 1.5
                }
            }
        ]
    },
    center: GEORGIA_CENTER,
    zoom: INITIAL_ZOOM
});

// Add navigation controls
map.addControl(new maplibregl.NavigationControl(), 'top-right');

// Track selected parcel (using feature_id as unique identifier across all counties)
let selectedFeatureId = null;

// Click handler for parcels
map.on('click', 'parcel-fill', (e) => {
    if (e.features.length === 0) return;

    const feature = e.features[0];
    
    // Clear previous selection
    if (selectedFeatureId !== null) {
        map.setFeatureState(
            { source: 'parcels', sourceLayer: 'parcels', id: selectedFeatureId },
            { selected: false }
        );
    }

    // Set new selection
    selectedFeatureId = feature.properties.feature_id;
    map.setFeatureState(
        { source: 'parcels', sourceLayer: 'parcels', id: selectedFeatureId },
        { selected: true }
    );

    // Create popup content
    const props = feature.properties;
    const popupHTML = `
        <div style="font-family: sans-serif; max-width: 300px;">
            <h3 style="margin: 0 0 10px 0; font-size: 14px; border-bottom: 2px solid #3388ff; padding-bottom: 5px;">
                Parcel ${props.site_number || 'N/A'}
            </h3>
            <table style="width: 100%; font-size: 12px;">
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Parcel ID:</td><td style="padding: 3px 0;">${props.parcel_id || 'N/A'}</td></tr>
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Address:</td><td style="padding: 3px 0;">${props.site_address || 'N/A'}</td></tr>
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Owner:</td><td style="padding: 3px 0;">${props.owner_name || 'N/A'}</td></tr>
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Owner Address:</td><td style="padding: 3px 0;">${props.owner_address || 'N/A'}</td></tr>
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Acres:</td><td style="padding: 3px 0;">${props.acres || 'N/A'}</td></tr>
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Classification:</td><td style="padding: 3px 0;">${props.classification || 'N/A'}</td></tr>
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Tax District:</td><td style="padding: 3px 0;">${props.tax_district || 'N/A'}</td></tr>
            </table>
        </div>
    `;

    // Show popup
    new maplibregl.Popup()
        .setLngLat(e.lngLat)
        .setHTML(popupHTML)
        .addTo(map);
});

// Helper function to format numbers with commas
function formatNumber(num) {
    if (num === null || num === undefined || num === '') return 'N/A';
    return Number(num).toLocaleString('en-US');
}

// County click handlers
map.on('click', 'county-fill-simplified', (e) => {
    if (e.features.length === 0) return;
    const props = e.features[0].properties;
    const popupHTML = `
        <div style="font-family: sans-serif; max-width: 250px;">
            <h3 style="margin: 0 0 10px 0; font-size: 14px; border-bottom: 2px solid #007BFF; padding-bottom: 5px;">
                ${props.name} County, ${props.state || 'GA'}
            </h3>
            <table style="width: 100%; font-size: 12px;">
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Population:</td><td style="padding: 3px 0;">${formatNumber(props.population)}</td></tr>
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Region:</td><td style="padding: 3px 0;">${props.region || 'N/A'}</td></tr>
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Acres:</td><td style="padding: 3px 0;">${formatNumber(props.acres)}</td></tr>
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Sq. Miles:</td><td style="padding: 3px 0;">${formatNumber(props.square_miles)}</td></tr>
            </table>
        </div>
    `;
    new maplibregl.Popup().setLngLat(e.lngLat).setHTML(popupHTML).addTo(map);
});

map.on('click', 'county-fill-full', (e) => {
    if (e.features.length === 0) return;
    const props = e.features[0].properties;
    const popupHTML = `
        <div style="font-family: sans-serif; max-width: 250px;">
            <h3 style="margin: 0 0 10px 0; font-size: 14px; border-bottom: 2px solid #007BFF; padding-bottom: 5px;">
                ${props.name} County, ${props.state || 'GA'}
            </h3>
            <table style="width: 100%; font-size: 12px;">
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Population:</td><td style="padding: 3px 0;">${formatNumber(props.population)}</td></tr>
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Region:</td><td style="padding: 3px 0;">${props.region || 'N/A'}</td></tr>
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Acres:</td><td style="padding: 3px 0;">${formatNumber(props.acres)}</td></tr>
                <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Sq. Miles:</td><td style="padding: 3px 0;">${formatNumber(props.square_miles)}</td></tr>
            </table>
        </div>
    `;
    new maplibregl.Popup().setLngLat(e.lngLat).setHTML(popupHTML).addTo(map);
});

// Change cursor on hover for counties
map.on('mouseenter', 'county-fill-simplified', () => {
    map.getCanvas().style.cursor = 'pointer';
});
map.on('mouseleave', 'county-fill-simplified', () => {
    map.getCanvas().style.cursor = '';
});
map.on('mouseenter', 'county-fill-full', () => {
    map.getCanvas().style.cursor = 'pointer';
});
map.on('mouseleave', 'county-fill-full', () => {
    map.getCanvas().style.cursor = '';
});

// Change cursor on hover for parcels
map.on('mouseenter', 'parcel-fill', () => {
    map.getCanvas().style.cursor = 'pointer';
});

map.on('mouseleave', 'parcel-fill', () => {
    map.getCanvas().style.cursor = '';
});

// Clear selection when clicking map background
map.on('click', (e) => {
    const features = map.queryRenderedFeatures(e.point, { layers: ['parcel-fill'] });
    if (features.length === 0 && selectedFeatureId !== null) {
        map.setFeatureState(
            { source: 'parcels', sourceLayer: 'parcels', id: selectedFeatureId },
            { selected: false }
        );
        selectedFeatureId = null;
    }
});

// Add zoom level indicator
const zoomDisplay = document.createElement('div');
zoomDisplay.style.cssText = 'position: absolute; bottom: 20px; right: 10px; background: rgba(255,255,255,0.9); padding: 8px 12px; border: 2px solid #333; border-radius: 4px; font-weight: bold; font-size: 14px; z-index: 1000;';
zoomDisplay.textContent = `Zoom: ${Math.round(map.getZoom() * 10) / 10}`;
document.body.appendChild(zoomDisplay);

map.on('zoom', () => {
    zoomDisplay.textContent = `Zoom: ${Math.round(map.getZoom() * 10) / 10}`;
});

// Load county GeoJSON when map is ready
map.on('load', () => {
    console.log('Map loaded successfully');
    console.log('Loading county boundaries...');
    
    // Load simplified boundaries (shown below zoom 11)
    fetch('http://localhost:9000/api/counties?detail=simplified')
        .then(response => response.json())
        .then(data => {
            map.addSource('counties-simplified', {
                type: 'geojson',
                data: data
            });
            
            map.addLayer({
                id: 'county-fill-simplified',
                type: 'fill',
                source: 'counties-simplified',
                minzoom: 0,
                maxzoom: 9,
                paint: {
                    'fill-color': '#88C0D0',
                    'fill-opacity': 0.5
                }
            });
            
            map.addLayer({
                id: 'county-outline-simplified',
                type: 'line',
                source: 'counties-simplified',
                minzoom: 0,
                maxzoom: 9,
                paint: {
                    'line-color': '#007BFF',
                    'line-width': 1
                }
            });
            
            // Add county name labels
            map.addLayer({
                id: 'county-labels',
                type: 'symbol',
                source: 'counties-simplified',
                maxzoom: 11,
                layout: {
                    'text-field': ['get', 'name'],
                    'text-size': 12,
                    'text-font': ['Noto Sans Regular'],
                    'text-anchor': 'center'
                },
                paint: {
                    'text-color': '#333333',
                    'text-halo-color': '#ffffff',
                    'text-halo-width': 2
                }
            });
            
            console.log('Simplified county boundaries loaded');
        })
        .catch(error => {
            console.error('Error loading simplified counties:', error);
        });
    
    // Load full boundaries (shown at zoom 11-15)
    fetch('http://localhost:9000/api/counties?detail=full')
        .then(response => response.json())
        .then(data => {
            map.addSource('counties-full', {
                type: 'geojson',
                data: data
            });
            
            map.addLayer({
                id: 'county-fill-full',
                type: 'fill',
                source: 'counties-full',
                minzoom: 9,
                maxzoom: 14,
                paint: {
                    'fill-color': '#88C0D0',
                    'fill-opacity': 0.2
                }
            });
            
            map.addLayer({
                id: 'county-outline-full',
                type: 'line',
                source: 'counties-full',
                minzoom: 9,
                maxzoom: 14,
                paint: {
                    'line-color': '#007BFF',
                    'line-width': 2
                }
            });
            
            console.log('Full county boundaries loaded');
        })
        .catch(error => {
            console.error('Error loading full counties:', error);
        });
});

// Debug: Log errors (filter out tile parsing errors for empty tiles)
map.on('error', (e) => {
    // Suppress tile parsing errors - these are expected for tiles with no/invalid data
    if (e.error && e.error.message && e.error.message.includes('Unable to parse the tile')) {
        return; // Silently ignore
    }
    console.error('Map error:', e);
});