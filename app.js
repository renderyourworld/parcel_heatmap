// MapLibre Vector Tiles
const GEORGIA_CENTER = [-83.6, 32.8]; // [lng, lat]
const INITIAL_ZOOM = 7;

// Available basemap styles
const BASEMAP_STYLES = [
    { id: 'light', name: 'Light', icon: '☀️' },
    { id: 'dark', name: 'Dark', icon: '🌙' },
    { id: 'black', name: 'Black', icon: '⬛' },
    { id: 'white', name: 'White', icon: '⬜' },
    { id: 'grayscale', name: 'Grayscale', icon: '🔘' }
];

let currentStyle = localStorage.getItem('mapStyle') || 'light'; // Load saved style or default

// Setup PMTiles Protocol
const protocol = new pmtiles.Protocol();
maplibregl.addProtocol('pmtiles', protocol.tile);

// Helper function to format numbers with commas
function formatNumber(num) {
    if (num === null || num === undefined || num === '') return 'N/A';
    return Number(num).toLocaleString('en-US');
}

// Device detection
function isMobile() {
    return window.innerWidth < 768;
}

// Geolocation state
let userLocationMarker = null;
let userLocationAccuracy = null;
let watchId = null;

// Get user's location (returns promise with {center, zoom} or defaults)
async function getUserLocation() {
    const defaults = { center: GEORGIA_CENTER, zoom: INITIAL_ZOOM };

    if (!navigator.geolocation) {
        console.log('Geolocation not supported');
        return defaults;
    }

    return new Promise((resolve) => {
        navigator.geolocation.getCurrentPosition(
            (position) => {
                const lng = position.coords.longitude;
                const lat = position.coords.latitude;
                const zoom = isMobile() ? 16 : 12;
                console.log(`Geolocation success: [${lng}, ${lat}], zoom: ${zoom}`);
                resolve({ center: [lng, lat], zoom, accuracy: position.coords.accuracy });
            },
            (error) => {
                console.log('Geolocation error:', error.message);
                resolve(defaults);
            },
            { enableHighAccuracy: true, timeout: 5000, maximumAge: 60000 }
        );
    });
}

async function loadMap() {
    const response = await fetch(`/styles/${currentStyle}.json`);
    const data = await response.json();
    const basemapLayers = data.layers;

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

    // County Boundary Layers (from vector tiles)
    const countyLayers = [
        {
            id: 'county-outline',
            type: 'line',
            source: 'counties',
            'source-layer': 'counties',
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
            id: 'county-fill',
            type: 'fill',
            source: 'counties',
            'source-layer': 'counties',
            maxzoom: 13,
            paint: {
                'fill-color': '#88C0D0',
                'fill-opacity': 0.4
            }
        },
        {
            id: 'county-labels',
            type: 'symbol',
            source: 'counties',
            'source-layer': 'counties',
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

    // Get user location before initializing map
    const userLocation = await getUserLocation();

    // Initialize the map
    window._tileCount = 0;

    const map = new maplibregl.Map({
        container: 'map',
        center: userLocation.center,
        zoom: userLocation.zoom,
        minZoom: 6,
        maxZoom: 19,
        transformRequest: (url) => {
            if (url.includes('/api/tiles/')) {
                window._tileCount++;
                if (!window._debugTileRequests) window._debugTileRequests = [];
                window._debugTileRequests.push(url);
            }
            return { url };
        },
        style: {
            version: 8,
            glyphs: '/fonts/{fontstack}/{range}.pbf',
            sprite: `${window.location.origin}/sprites/v4/${currentStyle}`,
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
                'counties': {
                    type: 'vector',
                    tiles: [`${window.location.origin}/api/tiles/counties/{z}/{x}/{y}`],
                    minzoom: 0,
                    maxzoom: 12,
                    promoteId: 'id'
                }
            },
            layers: [...basemapLayers, ...countyLayers, ...parcelLayers]
        },
    });

    window.map = map; // Expose map for debugging

    // Function to switch basemap styles
    async function setMapStyle(styleId) {
        if (styleId === currentStyle) return;

        const response = await fetch(`/styles/${styleId}.json`);
        const data = await response.json();
        const newBasemapLayers = data.layers;

        // Remove all existing basemap layers (layers from protomaps source that aren't our custom layers)
        const currentLayers = map.getStyle().layers;
        const customLayerIds = [...parcelLayers, ...countyLayers].map(l => l.id);

        currentLayers.forEach(layer => {
            if (!customLayerIds.includes(layer.id)) {
                map.removeLayer(layer.id);
            }
        });

        // Update the sprite
        map.setSprite(`${window.location.origin}/sprites/v4/${styleId}`);

        // Add new basemap layers at the bottom (before county layers)
        const firstCountyLayerId = countyLayers[0].id;
        newBasemapLayers.forEach(layer => {
            map.addLayer(layer, firstCountyLayerId);
        });

        currentStyle = styleId;
        localStorage.setItem('mapStyle', styleId); // Save preference

        // Update UI to reflect current selection
        document.querySelectorAll('.style-option').forEach(el => {
            el.classList.toggle('active', el.dataset.style === styleId);
        });
    }

    // Add navigation controls
    map.addControl(new maplibregl.NavigationControl(), 'top-right');

    // Add GeolocateControl for location tracking with custom positioning
    const geolocateControl = new maplibregl.GeolocateControl({
        positionOptions: { enableHighAccuracy: true },
        trackUserLocation: true,
        showUserLocation: true,
        showAccuracyCircle: true,
        showUserHeading: true  // Rotate map based on device compass/gyroscope
    });
    map.addControl(geolocateControl, 'top-right');

    // If we got user location on init, trigger the geolocate after map loads
    if (userLocation.accuracy) {
        map.once('load', () => {
            geolocateControl.trigger();
        });
    }

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
    map.on('click', 'county-fill', (e) => {
        if (e.features.length === 0) return;

        const feature = e.features[0];

        // Clear previous selection
        if (selectedCountyId !== null) {
            map.setFeatureState(
                { source: 'counties', sourceLayer: 'counties', id: selectedCountyId },
                { selected: false }
            );
        }

        // Set new selection
        selectedCountyId = feature.id;
        map.setFeatureState(
            { source: 'counties', sourceLayer: 'counties', id: selectedCountyId },
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
    map.on('mouseenter', 'county-fill', () => {
        map.getCanvas().style.cursor = 'pointer';
    });
    map.on('mouseleave', 'county-fill', () => {
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
        const countyFeatures = map.queryRenderedFeatures(e.point, { layers: ['county-fill'] });
        if (countyFeatures.length === 0 && selectedCountyId !== null) {
            map.setFeatureState(
                { source: 'counties', sourceLayer: 'counties', id: selectedCountyId },
                { selected: false }
            );
            selectedCountyId = null;
        }
    });

    // Add Style Switcher UI (top-right, below navigation controls)
    const styleSwitcher = document.createElement('div');
    styleSwitcher.className = 'style-switcher';
    styleSwitcher.style.cssText = `
        position: absolute;
        top: 150px;
        right: 10px;
        z-index: 1000;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    `;

    // Create the main button (compact layers icon)
    const mainButton = document.createElement('button');
    mainButton.className = 'style-main-btn';
    mainButton.title = 'Change map style';
    mainButton.style.cssText = `
        background: #fff;
        border: none;
        border-radius: 4px;
        width: 29px;
        height: 29px;
        cursor: pointer;
        display: flex;
        align-items: center;
        justify-content: center;
        box-shadow: 0 0 0 2px rgba(0,0,0,0.1);
        transition: background 0.15s ease;
    `;
    // Simple layers SVG icon
    mainButton.innerHTML = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#333" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 2 2 7 12 12 22 7 12 2"></polygon><polyline points="2 17 12 22 22 17"></polyline><polyline points="2 12 12 17 22 12"></polyline></svg>`;

    mainButton.addEventListener('mouseenter', () => {
        mainButton.style.background = '#f4f4f4';
    });
    mainButton.addEventListener('mouseleave', () => {
        mainButton.style.background = '#fff';
    });

    // Create dropdown container
    const dropdown = document.createElement('div');
    dropdown.className = 'style-dropdown';
    dropdown.style.cssText = `
        position: absolute;
        top: 0;
        right: 38px;
        background: #fff;
        border-radius: 4px;
        box-shadow: 0 0 0 2px rgba(0,0,0,0.1), 0 4px 12px rgba(0,0,0,0.15);
        opacity: 0;
        visibility: hidden;
        transform: translateX(8px);
        transition: all 0.15s ease;
        overflow: hidden;
        white-space: nowrap;
    `;

    // Add style options as compact horizontal buttons
    const optionsRow = document.createElement('div');
    optionsRow.style.cssText = 'display: flex; padding: 4px;';

    BASEMAP_STYLES.forEach(style => {
        const option = document.createElement('button');
        option.className = 'style-option';
        option.dataset.style = style.id;
        option.title = style.name;
        const isActive = style.id === currentStyle;
        option.style.cssText = `
            width: 28px;
            height: 28px;
            border: none;
            border-radius: 3px;
            background: ${isActive ? 'rgba(0, 123, 255, 0.15)' : 'transparent'};
            cursor: pointer;
            font-size: 14px;
            transition: all 0.1s ease;
            display: flex;
            align-items: center;
            justify-content: center;
        `;
        option.textContent = style.icon;

        option.addEventListener('mouseenter', () => {
            if (style.id !== currentStyle) {
                option.style.background = 'rgba(0, 0, 0, 0.05)';
            }
        });
        option.addEventListener('mouseleave', () => {
            option.style.background = style.id === currentStyle ? 'rgba(0, 123, 255, 0.15)' : 'transparent';
        });
        option.addEventListener('click', () => {
            setMapStyle(style.id);
            // Close dropdown
            dropdown.style.opacity = '0';
            dropdown.style.visibility = 'hidden';
            dropdown.style.transform = 'translateX(8px)';
        });

        optionsRow.appendChild(option);
    });

    dropdown.appendChild(optionsRow);

    // Toggle dropdown on click
    mainButton.addEventListener('click', (e) => {
        e.stopPropagation();
        const isVisible = dropdown.style.visibility === 'visible';
        dropdown.style.opacity = isVisible ? '0' : '1';
        dropdown.style.visibility = isVisible ? 'hidden' : 'visible';
        dropdown.style.transform = isVisible ? 'translateX(8px)' : 'translateX(0)';
    });

    // Close dropdown when clicking outside
    document.addEventListener('click', () => {
        dropdown.style.opacity = '0';
        dropdown.style.visibility = 'hidden';
        dropdown.style.transform = 'translateX(8px)';
    });

    styleSwitcher.appendChild(mainButton);
    styleSwitcher.appendChild(dropdown);
    document.body.appendChild(styleSwitcher);

    // Add zoom level indicator (top-right, below style switcher)
    const zoomDisplay = document.createElement('div');
    zoomDisplay.style.cssText = `
        position: absolute;
        top: 188px;
        right: 10px;
        background: #fff;
        padding: 4px 8px;
        border-radius: 4px;
        box-shadow: 0 0 0 2px rgba(0,0,0,0.1);
        font-weight: 500;
        font-size: 11px;
        color: #333;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
        z-index: 1000;
    `;
    zoomDisplay.textContent = `Z${Math.round(map.getZoom() * 10) / 10}`;
    document.body.appendChild(zoomDisplay);

    map.on('zoom', () => {
        zoomDisplay.textContent = `Z${Math.round(map.getZoom() * 10) / 10}`;
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