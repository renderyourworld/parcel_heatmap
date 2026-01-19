// MapLibre Vector Tiles
const GEORGIA_CENTER = [-83.6, 32.8]; // [lng, lat]
const INITIAL_ZOOM = 7;

// Setup PMTiles Protocol
const protocol = new pmtiles.Protocol();
maplibregl.addProtocol('pmtiles', protocol.tile);

// Helper function to format numbers with commas
function formatNumber(num) {
    if (num === null || num === undefined || num === '') return 'N/A';
    return Number(num).toLocaleString('en-US');
}

async function loadMap() {
    const response = await fetch('/styles/light.json');
    const data = await response.json();
    const basemapLayers = data.layers;

    // Preload county boundary data
    const [simplifiedCounties, fullCounties] = await Promise.all([
        fetch('/api/counties/simplified').then(r => r.json()),
        fetch('/api/counties/full').then(r => r.json())
    ]);

    // Ensure all county features have unique IDs for feature-state
    fullCounties.features.forEach((f, i) => {
        if (f.id === undefined || f.id === null) f.id = i;
    });

    // Parcel Layers
    const parcelLayers = [
        {
            id: 'parcel-fill',
            type: 'fill',
            source: 'parcels',
            'source-layer': 'parcels',
            minzoom: 13,
            paint: {
                'fill-color': ['case', ['boolean', ['feature-state', 'selected'], false], '#ff6b6b', '#3388ff'],
                'fill-opacity': ['case', ['boolean', ['feature-state', 'selected'], false], 0.6, 0.05]
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
                    ['boolean', ['feature-state', 'selected'], false], '#ff0000',
                    ['has', 'class_color'], ['get', 'class_color'],
                    '#3388ff'
                ],
                'line-width': ['case', ['boolean', ['feature-state', 'selected'], false], 2, 1]
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
    ];

    // County Boundary Layers
    const countyLayers = [
        {
            id: 'county-outline-simplified',
            type: 'line',
            source: 'counties-simplified',
            minzoom: 0,
            maxzoom: 9,
            paint: {
                'line-color': '#007BFF',
                'line-width': 1
            }
        },
        {
            id: 'county-outline-full',
            type: 'line',
            source: 'counties-full',
            minzoom: 9,
            maxzoom: 19,
            paint: {
                'line-color': [
                    'case',
                    ['boolean', ['feature-state', 'selected'], false], '#ff0000',
                    '#007BFF'
                ],
                'line-width': ['case', ['boolean', ['feature-state', 'selected'], false], 2, 1]
            }
        },
        {
            id: 'county-fill-full',
            type: 'fill',
            source: 'counties-full',
            minzoom: 0,
            maxzoom: 13,
            paint: {
                'fill-color': '#88C0D0',
                'fill-opacity': 0.4
            }
        },
        {
            id: 'county-labels',
            type: 'symbol',
            source: 'counties-simplified',
            maxzoom: 13,
            layout: {
                'text-field': ['get', 'name'],
                'text-size': 12,
                'text-font': ['Noto Sans Regular'],
                'text-anchor': 'center',
                'text-letter-spacing': 0.05
            },
            paint: {
                'text-color': '#000000'
            }
        }
    ];

    // Initialize the map
    window._tileCount = 0;
    const map = new maplibregl.Map({
        container: 'map',
        center: GEORGIA_CENTER,
        zoom: INITIAL_ZOOM,
        minZoom: 6,
        maxZoom: 19,
        transformRequest: (url) => {
            if (url.includes('/api/tiles/')) {
                window._tileCount++;
            }
            return { url };
        },
        style: {
            version: 8,
            glyphs: 'https://protomaps.github.io/basemaps-assets/fonts/{fontstack}/{range}.pbf',
            sprite: "https://protomaps.github.io/basemaps-assets/sprites/v4/light", // /v4/dark for dark mode
            sources: {
                'protomaps': {
                    type: 'vector',
                    url: `pmtiles:///georgia.pmtiles`,
                    attribution: '<a href="https://protomaps.com">Protomaps</a> © <a href="https://openstreetmap.org">OpenStreetMap</a>'
                },
                'parcels': {
                    type: 'vector',
                    tiles: [`${window.location.origin}/api/tiles/{z}/{x}/{y}`],
                    minzoom: 13,
                    maxzoom: 19,
                    promoteId: 'feature_id'
                },
                'counties-simplified': {
                    type: 'geojson',
                    data: simplifiedCounties
                },
                'counties-full': {
                    type: 'geojson',
                    data: fullCounties
                }
            },
            layers: [...basemapLayers, ...countyLayers, ...parcelLayers]
        },
    });

    window.map = map; // Expose map for debugging

    // Add navigation controls
    map.addControl(new maplibregl.NavigationControl(), 'top-right');

    // Track selected parcel (using feature_id as unique identifier across all counties)
    let selectedFeatureId = null;
    let selectedCountyId = null;

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
                    ${props.site_address || `Parcel: ${props.parcel_id}`}
                </h3>
                <table style="width: 100%; font-size: 12px;">
                    <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Parcel ID:</td><td style="padding: 3px 0;">${props.parcel_id || 'N/A'}</td></tr>
                    <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Address:</td><td style="padding: 3px 0;">${props.site_address || 'N/A'}</td></tr>
                    <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Owner:</td><td style="padding: 3px 0;">${props.owner_name || 'N/A'}</td></tr>
                    <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Owner Address:</td><td style="padding: 3px 0;">${props.owner_address || 'N/A'}</td></tr>
                    <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Acres:</td><td style="padding: 3px 0;">${props.acres || 'N/A'}</td></tr>
                    <tr><td style="padding: 3px 5px 3px 0; font-weight: bold;">Class:</td><td style="padding: 3px 0;">${props.category || 'N/A'} (${props.classification || 'N/A'})</td></tr>
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

    // County click handlers
    map.on('click', 'county-fill-full', (e) => {
        if (e.features.length === 0) return;

        const feature = e.features[0];

        // Clear previous selection
        if (selectedCountyId !== null) {
            map.setFeatureState(
                { source: 'counties-full', id: selectedCountyId },
                { selected: false }
            );
        }

        // Set new selection
        selectedCountyId = feature.id;
        map.setFeatureState(
            { source: 'counties-full', id: selectedCountyId },
            { selected: true }
        );

        const props = feature.properties;
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
        // Handle parcel deselection
        const parcelFeatures = map.queryRenderedFeatures(e.point, { layers: ['parcel-fill'] });
        if (parcelFeatures.length === 0 && selectedFeatureId !== null) {
            map.setFeatureState(
                { source: 'parcels', sourceLayer: 'parcels', id: selectedFeatureId },
                { selected: false }
            );
            selectedFeatureId = null;
        }

        // Handle county deselection
        const countyFeatures = map.queryRenderedFeatures(e.point, { layers: ['county-fill-full'] });
        if (countyFeatures.length === 0 && selectedCountyId !== null) {
            map.setFeatureState(
                { source: 'counties-full', id: selectedCountyId },
                { selected: false }
            );
            selectedCountyId = null;
        }
    });

    // Add controls container
    const controls = document.createElement('div');
    controls.style.cssText = `
        position: absolute; 
        bottom: 20px; 
        left: 10px; 
        background: rgba(255, 255, 255, 0.85); 
        backdrop-filter: blur(8px);
        padding: 12px; 
        border-radius: 12px; 
        box-shadow: 0 4px 15px rgba(0,0,0,0.15); 
        border: 1px solid rgba(255,255,255,0.4);
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; 
        z-index: 1000; 
        display: flex; 
        flex-direction: column; 
        gap: 10px;
    `;
    document.body.appendChild(controls);

    // Add Tile Borders toggle
    const borderToggleContainer = document.createElement('label');
    borderToggleContainer.style.cssText = 'display: flex; align-items: center; gap: 10px; cursor: pointer; font-size: 14px; color: #1a1a1a; font-weight: 500;';

    const borderCheckbox = document.createElement('input');
    borderCheckbox.type = 'checkbox';
    borderCheckbox.style.cssText = 'width: 16px; height: 16px; cursor: pointer; accent-color: #007BFF;';
    borderCheckbox.addEventListener('change', (e) => {
        map.showTileBoundaries = e.target.checked;
    });

    const borderText = document.createElement('span');
    borderText.textContent = 'Show Tile Borders';

    borderToggleContainer.appendChild(borderCheckbox);
    borderToggleContainer.appendChild(borderText);
    controls.appendChild(borderToggleContainer);

    // Add zoom level indicator
    const zoomDisplay = document.createElement('div');
    zoomDisplay.style.cssText = `
        position: absolute; 
        bottom: 20px; 
        right: 10px; 
        background: rgba(255, 255, 255, 0.85); 
        backdrop-filter: blur(8px);
        padding: 8px 16px; 
        border-radius: 12px; 
        box-shadow: 0 4px 15px rgba(0,0,0,0.15); 
        border: 1px solid rgba(255,255,255,0.4);
        font-weight: 600; 
        font-size: 13px; 
        color: #1a1a1a;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
        z-index: 1000;
    `;
    zoomDisplay.textContent = `Zoom: ${Math.round(map.getZoom() * 10) / 10}`;
    document.body.appendChild(zoomDisplay);

    map.on('zoom', () => {
        zoomDisplay.textContent = `Zoom: ${Math.round(map.getZoom() * 10) / 10}`;
    });

    // Debug: Log errors (filter out tile parsing errors for empty tiles)
    map.on('error', (e) => {
        // Suppress tile parsing errors - these are expected for tiles with no/invalid data
        if (e.error && e.error.message && e.error.message.includes('Unable to parse the tile')) {
            return; // Silently ignore
        }
        console.error('Map error:', e);
    });
}

loadMap();