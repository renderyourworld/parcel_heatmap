// MapLibre Vector Tiles with OpenFreeMap
const GEORGIA_CENTER = [-83.6, 32.8]; // [lng, lat]
const INITIAL_ZOOM = 7;

// Load theme preference from localStorage or detect system preference
let currentTheme = localStorage.getItem('mapTheme');
if (!currentTheme) {
    currentTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

// Initialize the map with OpenFreeMap style
const map = new maplibregl.Map({
    container: 'map',
    maxZoom: 19,
    style: currentTheme === 'dark' 
        ? 'https://tiles.openfreemap.org/styles/dark'
        : 'https://tiles.openfreemap.org/styles/liberty',
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

// Pre-fetch and cache county boundaries
console.log('Pre-fetching county boundaries...');
let simplifiedCountiesData = null;
let fullCountiesData = null;
const simplifiedCountiesPromise = fetch('http://localhost:9000/api/counties?detail=simplified')
    .then(r => r.json())
    .then(data => { simplifiedCountiesData = data; return data; });
const fullCountiesPromise = fetch('http://localhost:9000/api/counties?detail=full')
    .then(r => r.json())
    .then(data => { fullCountiesData = data; return data; });

// Load county GeoJSON and add parcel layers when map is ready
map.on('load', () => {
    console.log('Map loaded successfully');
    
    // Wait for county data to be fetched, then add all layers
    Promise.all([simplifiedCountiesPromise, fullCountiesPromise])
        .then(() => {
            addCustomLayers();
            console.log('All layers added');
        })
        .catch(error => {
            console.error('Error loading county data:', error);
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

// Theme toggle functionality
const themeToggle = document.getElementById('themeToggle');

function addCustomLayers() {
    // Add parcel source and layers
    map.addSource('parcels', {
        type: 'vector',
        tiles: ['http://localhost:9000/api/tiles/{z}/{x}/{y}'],
        minzoom: 13,
        maxzoom: 19,
        promoteId: 'feature_id'
    });
    
    map.addLayer({
        id: 'parcel-fill',
        type: 'fill',
        source: 'parcels',
        'source-layer': 'parcels',
        minzoom: 13,
        paint: {
            'fill-color': ['case', ['boolean', ['feature-state', 'selected'], false], '#ff6b6b', '#3388ff'],
            'fill-opacity': 0.4
        }
    });
    
    map.addLayer({
        id: 'parcel-outline',
        type: 'line',
        source: 'parcels',
        'source-layer': 'parcels',
        minzoom: 13,
        paint: {
            'line-color': ['case', ['boolean', ['feature-state', 'selected'], false], '#ff0000', '#3388ff'],
            'line-width': ['case', ['boolean', ['feature-state', 'selected'], false], 2, 0.5]
        }
    });
    
    map.addLayer({
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
            'text-optional': true
        },
        paint: {
            'text-color': '#333333',
            'text-halo-color': '#ffffff',
            'text-halo-width': 1.5
        }
    });
    
    // Override road label layers to show at zoom 12+
    const style = map.getStyle();
    if (style && style.layers) {
        style.layers.forEach(layer => {
            if (layer.type === 'symbol' && 
                layer['source-layer'] === 'transportation_name' &&
                layer.layout && layer.layout['text-field']) {
                map.setLayerZoomRange(layer.id, 12, 24);
            }
        });
    }
    
    // Add county boundaries (use cached data)
    if (simplifiedCountiesData) {
        map.addSource('counties-simplified', {
            type: 'geojson',
            data: simplifiedCountiesData
        });
        
        map.addLayer({
            id: 'county-fill-simplified',
            type: 'fill',
            source: 'counties-simplified',
            minzoom: 0,
            maxzoom: 11,
            paint: { 'fill-color': '#88C0D0', 'fill-opacity': 0.5 }
        });
        
        map.addLayer({
            id: 'county-outline-simplified',
            type: 'line',
            source: 'counties-simplified',
            minzoom: 0,
            maxzoom: 11,
            paint: { 'line-color': '#007BFF', 'line-width': 0.5 }
        });
        
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
            paint: { 'text-color': '#333333', 'text-halo-color': '#ffffff', 'text-halo-width': 2 }
        });
    }
    
    if (fullCountiesData) {
        map.addSource('counties-full', {
            type: 'geojson',
            data: fullCountiesData
        });
        
        map.addLayer({
            id: 'county-outline-full',
            type: 'line',
            source: 'counties-full',
            minzoom: 10,
            maxzoom: 13,
            paint: { 'line-color': '#007BFF', 'line-width': 1 }
        });
    }
}

function updateTheme(theme) {
    const styleUrl = theme === 'dark' 
        ? 'https://tiles.openfreemap.org/styles/dark'
        : 'https://tiles.openfreemap.org/styles/liberty';
    
    // Save current map state
    const center = map.getCenter();
    const zoom = map.getZoom();
    const bearing = map.getBearing();
    const pitch = map.getPitch();
    
    // Switch style (preserving transformRequest)
    map.setStyle(styleUrl, { diff: false });
    
    // Re-add custom layers after style loads
    map.once('styledata', () => {
        // Restore position first
        map.jumpTo({
            center: center,
            zoom: zoom,
            bearing: bearing,
            pitch: pitch
        });
        
        // Add custom layers
        addCustomLayers();
        console.log('Theme switched to', theme);
    });
    
    // Update button appearance
    if (theme === 'dark') {
        themeToggle.textContent = '☀️ Light Mode';
        themeToggle.classList.add('dark');
    } else {
        themeToggle.textContent = '🌙 Dark Mode';
        themeToggle.classList.remove('dark');
    }
    
    // Save preference
    localStorage.setItem('mapTheme', theme);
    currentTheme = theme;
}

// Initialize button state
if (currentTheme === 'dark') {
    themeToggle.textContent = '☀️ Light Mode';
    themeToggle.classList.add('dark');
} else {
    themeToggle.textContent = '🌙 Dark Mode';
    themeToggle.classList.remove('dark');
}

// Toggle theme on button click
themeToggle.addEventListener('click', () => {
    const newTheme = currentTheme === 'light' ? 'dark' : 'light';
    updateTheme(newTheme);
});

// Listen for system theme changes
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
    if (!localStorage.getItem('mapTheme')) { // Only auto-switch if user hasn't manually chosen
        updateTheme(e.matches ? 'dark' : 'light');
    }
});