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

function formatLocalDateTime(value) {
    if (!value) return 'N/A';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return 'N/A';
    return date.toLocaleString();
}

function escapeHtml(str) {
    if (str === null || str === undefined) return '';
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

function getPopupTheme(currentStyle) {
    const darkTheme = currentStyle === 'dark' || currentStyle === 'black';
    return darkTheme
        ? {
            bg: 'rgba(18, 24, 32, 0.97)',
            text: '#e8edf3',
            muted: '#b7c4d1',
            border: 'rgba(255,255,255,0.08)',
            shadow: '0 10px 24px rgba(0,0,0,0.45)'
        }
        : {
            bg: 'rgba(255, 255, 255, 0.98)',
            text: '#18222d',
            muted: '#526170',
            border: 'rgba(0,0,0,0.08)',
            shadow: '0 10px 24px rgba(0,0,0,0.16)'
        };
}

function createPopupHTML({ currentStyle, title, accent, rows, maxWidth = 320, footerText = '', footerHtml = '' }) {
    const t = getPopupTheme(currentStyle);
    const mobile = isMobile();
    const labelColWidth = mobile ? 78 : 96;
    const rowPadY = mobile ? 4 : 5;
    const titleSize = mobile ? 14 : 15;
    const bodySize = mobile ? 12 : 13;
    const cardPadTop = mobile ? 10 : 12;
    const cardPadSide = mobile ? 10 : 12;
    const cardPadBottom = mobile ? 7 : 8;
    const mobileCap = Math.min(maxWidth, 320);
    const effectiveMaxWidth = mobile
        ? `min(${mobileCap}px, calc(100vw - 24px))`
        : `${maxWidth}px`;

    const rowHTML = rows.map(r => `
        <div style="display:grid; grid-template-columns: ${labelColWidth}px 1fr; column-gap:10px; padding:${rowPadY}px 0; border-bottom:1px solid ${t.border};">
            <div style="font-weight:600; color:${t.text};">${r.label}</div>
            <div style="color:${t.muted};">${r.value}</div>
        </div>
    `).join('');
    let footerBlock = '';
    if (footerHtml) {
        footerBlock = `<div style="margin-top:8px; font-size:${mobile ? 11 : 11.5}px; color:${t.muted}; opacity:0.95;">${footerHtml}</div>`;
    } else if (footerText) {
        footerBlock = `<div style="margin-top:8px; font-size:${mobile ? 11 : 11.5}px; color:${t.muted}; opacity:0.9;">${footerText}</div>`;
    }

    return `
        <div style="
            font-family: 'Noto Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
            max-width:${effectiveMaxWidth};
            font-size:${bodySize}px;
            color:${t.text};
            background:${t.bg};
            border:1px solid ${t.border};
            border-radius:12px;
            box-shadow:${t.shadow};
            padding:${cardPadTop}px ${cardPadSide}px ${cardPadBottom}px ${cardPadSide}px;
            backdrop-filter: blur(2px);
        ">
            <div style="
                margin:0 0 10px 0;
                font-size:${titleSize}px;
                font-weight:700;
                letter-spacing:0.01em;
                padding-bottom:8px;
                border-bottom:2px solid ${accent};
                color:${t.text};
            ">${title}</div>
            <div>${rowHTML}</div>
            ${footerBlock}
        </div>
    `;
}

function toNumberOrNull(value) {
    if (value === null || value === undefined || value === '') return null;
    const n = Number(value);
    return Number.isFinite(n) ? n : null;
}

function buildParcelFooter({ currentStyle, lat, lng, updatedAt }) {
    const t = getPopupTheme(currentStyle);
    const parsedLat = toNumberOrNull(lat);
    const parsedLng = toNumberOrNull(lng);
    const hasCoords = parsedLat !== null && parsedLng !== null;
    const mapsHref = hasCoords
        ? `https://www.google.com/maps?q=${parsedLat.toFixed(6)},${parsedLng.toFixed(6)}`
        : '';
    const linkColor = t.muted;
    const updatedLine = `Last Updated At: ${formatLocalDateTime(updatedAt)}`;

    const mapsLine = hasCoords
        ? `<a href="${mapsHref}" target="_blank" rel="noopener noreferrer" style="display:inline-flex; align-items:center; gap:6px; color:${linkColor}; text-decoration:none; margin-bottom:6px;">
                <svg width="13" height="13" viewBox="0 0 24 24" aria-hidden="true" style="display:block;">
                    <path fill="currentColor" d="M12 2a7 7 0 0 0-7 7c0 5.25 7 13 7 13s7-7.75 7-13a7 7 0 0 0-7-7zm0 9.25A2.25 2.25 0 1 1 12 6.75a2.25 2.25 0 0 1 0 4.5z"/>
                </svg>
                <span>Open in Google Maps</span>
           </a>`
        : '';

    return `${mapsLine}<div>${updatedLine}</div>`;
}

function ensurePopupBaseStyles() {
    if (document.getElementById('custom-map-popup-styles')) return;

    const style = document.createElement('style');
    style.id = 'custom-map-popup-styles';
    style.textContent = `
        .maplibregl-popup-content {
            background: transparent !important;
            padding: 0 !important;
            box-shadow: none !important;
            border-radius: 0 !important;
        }
        .maplibregl-popup-tip {
            border-top-color: transparent !important;
            border-bottom-color: transparent !important;
        }
        .maplibregl-popup-close-button {
            color: #7f8ea0;
            font-size: 16px;
            right: 6px;
            top: 4px;
            line-height: 1;
            width: 28px;
            height: 28px;
            display: grid;
            place-items: center;
            border-radius: 8px;
        }
        .maplibregl-popup-close-button:hover {
            color: #c4ced8;
            background: rgba(127, 142, 160, 0.12);
        }
        .popup-skeleton-line {
            display: inline-block;
            height: 10px;
            border-radius: 999px;
            background: linear-gradient(90deg, rgba(140,155,170,0.18) 25%, rgba(190,205,220,0.38) 50%, rgba(140,155,170,0.18) 75%);
            background-size: 200% 100%;
            animation: popupShimmer 1.2s ease-in-out infinite;
            vertical-align: middle;
        }
        .popup-skeleton-short { width: 72px; }
        .popup-skeleton-medium { width: 120px; }
        .popup-skeleton-long { width: 165px; }
        @keyframes popupShimmer {
            0% { background-position: 200% 0; }
            100% { background-position: -200% 0; }
        }
    `;
    document.head.appendChild(style);
}

function ensureSearchStyles() {
    if (document.getElementById('map-search-styles')) return;

    const style = document.createElement('style');
    style.id = 'map-search-styles';
    style.textContent = `
        .map-search {
            position: absolute;
            top: 12px;
            left: 12px;
            z-index: 1100;
            width: 360px;
            font-family: 'Noto Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
        }
        .map-search-input-wrap {
            display: grid;
            grid-template-columns: 1fr auto;
            align-items: center;
            gap: 8px;
            background: rgba(255, 255, 255, 0.96);
            border: 1px solid rgba(0, 0, 0, 0.12);
            border-radius: 12px;
            padding: 8px 10px;
            box-shadow: 0 8px 20px rgba(0, 0, 0, 0.15);
            backdrop-filter: blur(2px);
        }
        .map-search-input {
            border: none;
            outline: none;
            width: 100%;
            font-size: 16px;
            background: transparent;
            color: #192532;
        }
        .map-search-clear {
            width: 24px;
            height: 24px;
            border: none;
            border-radius: 6px;
            background: transparent;
            color: #6f7f91;
            cursor: pointer;
            font-size: 18px;
            line-height: 1;
            display: grid;
            place-items: center;
        }
        .map-search-clear:hover {
            background: rgba(0, 0, 0, 0.08);
        }
        .map-search-results {
            margin-top: 8px;
            background: rgba(255, 255, 255, 0.98);
            border: 1px solid rgba(0, 0, 0, 0.12);
            border-radius: 12px;
            box-shadow: 0 10px 24px rgba(0, 0, 0, 0.18);
            overflow: hidden;
            display: none;
            max-height: 340px;
            overflow-y: auto;
        }
        .map-search-item {
            display: block;
            width: 100%;
            text-align: left;
            border: none;
            background: transparent;
            padding: 10px 12px;
            border-bottom: 1px solid rgba(0, 0, 0, 0.08);
            cursor: pointer;
        }
        .map-search-item:last-child {
            border-bottom: none;
        }
        .map-search-item:hover,
        .map-search-item.active {
            background: rgba(47, 110, 169, 0.10);
        }
        .map-search-line1 {
            font-size: 14px;
            color: #16222f;
        }
        .map-search-line2 {
            margin-top: 2px;
            font-size: 12px;
            color: #5c6b7a;
        }
        .map-search-empty {
            padding: 12px;
            font-size: 13px;
            color: #5c6b7a;
        }
        .map-search-modes {
            margin-top: 6px;
            display: inline-flex;
            gap: 4px;
            padding: 3px;
            border-radius: 10px;
            background: rgba(255, 255, 255, 0.92);
            border: 1px solid rgba(0, 0, 0, 0.10);
        }
        .map-search-mode {
            border: none;
            border-radius: 8px;
            padding: 4px 10px;
            font-size: 12px;
            color: #4e5d6d;
            background: transparent;
            cursor: pointer;
        }
        .map-search-mode.active {
            color: #ffffff;
            background: #2f6ea9;
        }
        body.map-theme-dark .map-search-input-wrap,
        body.map-theme-dark .map-search-results {
            background: rgba(18, 24, 32, 0.96);
            border-color: rgba(255, 255, 255, 0.12);
        }
        body.map-theme-dark .map-search-modes {
            background: rgba(18, 24, 32, 0.96);
            border-color: rgba(255, 255, 255, 0.12);
        }
        body.map-theme-dark .map-search-mode {
            color: #c2cdda;
        }
        body.map-theme-dark .map-search-mode.active {
            color: #ffffff;
            background: #4f88bf;
        }
        body.map-theme-dark .map-search-input {
            color: #e7edf3;
        }
        body.map-theme-dark .map-search-line1 {
            color: #e7edf3;
        }
        body.map-theme-dark .map-search-line2,
        body.map-theme-dark .map-search-empty,
        body.map-theme-dark .map-search-clear {
            color: #b5c3d1;
        }
        body.map-theme-dark .map-search-item {
            border-bottom-color: rgba(255, 255, 255, 0.08);
        }
        body.map-theme-dark .map-search-item:hover,
        body.map-theme-dark .map-search-item.active {
            background: rgba(81, 144, 209, 0.18);
        }
        body.map-theme-dark .map-search-clear:hover {
            background: rgba(255, 255, 255, 0.08);
        }
        @media (max-width: 767px) {
            .map-search {
                left: 10px;
                right: 56px;
                width: auto;
            }
            .map-search-input {
                font-size: 15px;
            }
        }
    `;
    document.head.appendChild(style);
}

// Device detection
function isMobile() {
    return window.innerWidth < 768;
}

// Compass rotation — rotates the map bearing to match the phone's compass heading.
// Uses DeviceOrientation API and sets bearing directly on MapLibre's transform
// to avoid conflicts with user gestures (zoom, pan).
const compass = {
    state: 'OFF', // 'OFF' | 'COMPASS'
    heading: null,
    _handler: null,
    _raf: null,
    _permitted: false,

    isAvailable() {
        return 'DeviceOrientationEvent' in window || 'ondeviceorientationabsolute' in window;
    },

    async requestPermission() {
        if (this._permitted) return;
        // iOS 13+ requires explicit permission on user gesture
        if (typeof DeviceOrientationEvent !== 'undefined' &&
            typeof DeviceOrientationEvent.requestPermission === 'function') {
            try {
                const result = await DeviceOrientationEvent.requestPermission();
                this._permitted = (result === 'granted');
            } catch (err) {
                console.log('Compass permission denied:', err);
            }
        } else {
            this._permitted = true;
        }
    },

    _eventToHeading(event) {
        // iOS Safari provides webkitCompassHeading directly (degrees CW from north)
        if (event.webkitCompassHeading != null) return event.webkitCompassHeading;
        // Android / standard: alpha is degrees CCW from north
        if (event.alpha != null) return (360 - event.alpha) % 360;
        return null;
    },

    start(map) {
        if (this._handler) return;

        let pending = null;
        const self = this;

        this._handler = function(event) {
            const heading = self._eventToHeading(event);
            if (heading === null) return;
            pending = heading;

            if (!self._raf) {
                self._raf = requestAnimationFrame(function() {
                    self._raf = null;
                    if (self.state !== 'COMPASS' || pending === null) return;
                    self.heading = pending;
                    map.transform.setBearing(pending);
                    map.fire('rotate');
                    map.triggerRepaint();
                });
            }
        };

        if ('ondeviceorientationabsolute' in window) {
            window.addEventListener('deviceorientationabsolute', this._handler);
        } else {
            window.addEventListener('deviceorientation', this._handler);
        }
        this.state = 'COMPASS';
    },

    stop() {
        if (this._handler) {
            window.removeEventListener('deviceorientationabsolute', this._handler);
            window.removeEventListener('deviceorientation', this._handler);
            this._handler = null;
        }
        if (this._raf) {
            cancelAnimationFrame(this._raf);
            this._raf = null;
        }
        this.heading = null;
        this.state = 'OFF';
    }
};

// Set up compass-based map rotation tied to the GeolocateControl lifecycle.
// - Replaces _updateCamera with direct transform calls (avoids canceling gestures)
// - Auto-starts compass when tracking is active (ACTIVE_LOCK)
// - Stops compass on user drag or when tracking ends
function setupCompassTracking(map, geolocateControl) {
    if (!compass.isAvailable()) return;

    // _updateCamera is only called in ACTIVE_LOCK state. We replace the default
    // fitBounds() implementation which fires events and cancels ongoing animations.
    geolocateControl._updateCamera = function(position) {
        this._map.transform.setCenter(new maplibregl.LngLat(
            position.coords.longitude,
            position.coords.latitude
        ));
        this._map.triggerRepaint();

        // Auto-start compass when entering ACTIVE_LOCK
        if (compass.state === 'OFF' && compass._permitted) {
            compass.start(this._map);
        }
    };

    // Request permission eagerly so it's ready before the first compass start
    geolocateControl.on('trackuserlocationstart', () => {
        compass.requestPermission();
    });

    // Stop compass when user drags (control transitions to BACKGROUND)
    map.on('dragstart', (e) => {
        if (e.originalEvent && compass.state === 'COMPASS') {
            compass.stop();
        }
    });

    // Stop compass when tracking ends entirely
    geolocateControl.on('trackuserlocationend', () => {
        if (compass.state === 'COMPASS') {
            compass.stop();
        }
    });
}

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
    ensurePopupBaseStyles();
    ensureSearchStyles();

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
                'fill-color': ['case', ['boolean', ['feature-state', 'selected'], false], '#e4572e', '#2f6ea9'],
                'fill-opacity': ['interpolate', ['linear'], ['zoom'],
                    13, ['case', ['boolean', ['feature-state', 'selected'], false], 0.42, 0.03],
                    16, ['case', ['boolean', ['feature-state', 'selected'], false], 0.42, 0.06],
                    19, ['case', ['boolean', ['feature-state', 'selected'], false], 0.42, 0.09]
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
                    ['boolean', ['feature-state', 'selected'], false], '#e4572e',
                    ['has', 'class_color'], ['get', 'class_color'],
                    '#2f6ea9'
                ],
                'line-width': ['interpolate', ['linear'], ['zoom'],
                    13, ['case', ['boolean', ['feature-state', 'selected'], false], 2.2, 0.6],
                    16, ['case', ['boolean', ['feature-state', 'selected'], false], 2.2, 1],
                    19, ['case', ['boolean', ['feature-state', 'selected'], false], 2.2, 1.3]
                ],
                'line-opacity': 0.9
            }
        },
        {
            id: 'parcel-labels',
            type: 'symbol',
            source: 'parcels',
            'source-layer': 'parcel_labels',
            minzoom: 15,
            layout: {
                'text-field': ['get', 'site_number'],
                'text-size': ['interpolate', ['linear'], ['zoom'], 15, 8, 17, 9.5, 19, 11],
                'text-font': ['Noto Sans Regular'],
                'text-anchor': 'center',
                'text-padding': 2,
                'symbol-avoid-edges': true,
                'symbol-z-order': 'auto',
                'symbol-placement': 'point',
                'text-allow-overlap': false,
                'text-ignore-placement': false,
                'text-optional': true
            },
            paint: {
                'text-color': '#23313d',
                'text-halo-color': '#ffffff',
                'text-halo-width': 1.2,
                'text-halo-blur': 0.3
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
                    ['boolean', ['feature-state', 'selected'], false], '#e4572e',
                    '#5f88a6'
                ],
                'line-width': ['interpolate', ['linear'], ['zoom'],
                    6, ['case', ['boolean', ['feature-state', 'selected'], false], 2.2, 0.7],
                    9, ['case', ['boolean', ['feature-state', 'selected'], false], 2.2, 0.9],
                    12, ['case', ['boolean', ['feature-state', 'selected'], false], 2.2, 1.2]
                ],
                'line-opacity': ['interpolate', ['linear'], ['zoom'], 6, 0.55, 9, 0.72, 12, 0.88]
            }
        },
        {
            id: 'county-fill',
            type: 'fill',
            source: 'counties',
            'source-layer': 'counties',
            maxzoom: 13,
            paint: {
                'fill-color': '#7fb3d5',
                'fill-opacity': ['interpolate', ['linear'], ['zoom'], 6, 0.12, 9, 0.08, 12.5, 0.02, 13, 0]
            }
        },
        {
            id: 'county-labels',
            type: 'symbol',
            source: 'counties',
            'source-layer': 'county_labels',
            maxzoom: 13,
            layout: {
                'text-field': ['get', 'name'],
                'text-size': ['interpolate', ['linear'], ['zoom'], 6, 10, 9, 12.5, 12, 14],
                'text-font': ['Noto Sans Regular'],
                'text-anchor': 'center',
                'text-letter-spacing': 0.04,
                'text-padding': 3,
                'symbol-avoid-edges': true,
                'text-allow-overlap': false
            },
            paint: {
                'text-color': '#1f2a33',
                'text-halo-color': 'rgba(255,255,255,0.9)',
                'text-halo-width': 1.1,
                'text-halo-blur': 0.35
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
        minZoom: 5,
        maxZoom: 19,
        crossSourceCollisions: false,
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

    // Track selected features for map highlighting.
    let selectedFeatureId = null;
    let selectedCountyId = null;
    let activePopup = null;
    let geolocateControl = null;
    const parcelDetailsCache = new Map();

    function clearActivePopup() {
        if (activePopup) {
            activePopup.remove();
            activePopup = null;
        }
    }

    function setActivePopup(popup) {
        clearActivePopup();
        activePopup = popup;
        popup.on('close', () => {
            if (activePopup === popup) {
                activePopup = null;
            }
        });
    }

    function clearMapSelection() {
        if (selectedFeatureId !== null) {
            map.setFeatureState(
                { source: 'parcels', sourceLayer: 'parcels', id: selectedFeatureId },
                { selected: false }
            );
            selectedFeatureId = null;
        }
        if (selectedCountyId !== null) {
            map.setFeatureState(
                { source: 'counties', sourceLayer: 'counties', id: selectedCountyId },
                { selected: false }
            );
            selectedCountyId = null;
        }
    }

    function stopSearchFlyTrackingConflicts() {
        // Keep search flyTo behavior consistent with manual pan: break heading lock.
        if (compass.state === 'COMPASS') {
            compass.stop();
        }

        // ACTIVE_LOCK can fight flyTo animation; toggle it off using control internals.
        if (geolocateControl && geolocateControl._watchState === 'ACTIVE_LOCK') {
            if (typeof geolocateControl._onControlClick === 'function') {
                geolocateControl._onControlClick();
            } else if (geolocateControl._geolocateButton) {
                geolocateControl._geolocateButton.click();
            }
        }
    }

    // Address search UI (DB-backed autocomplete).
    const searchContainer = document.createElement('div');
    searchContainer.className = 'map-search';
    searchContainer.innerHTML = `
        <div class="map-search-input-wrap">
            <input class="map-search-input" type="text" placeholder="Search Georgia address..." autocomplete="off" />
            <button class="map-search-clear" title="Clear search" aria-label="Clear search">&times;</button>
        </div>
        <div class="map-search-modes">
            <button class="map-search-mode active" data-mode="address">Address</button>
            <button class="map-search-mode" data-mode="owner">Owner</button>
        </div>
        <div class="map-search-results"></div>
    `;
    document.body.appendChild(searchContainer);

    const searchInput = searchContainer.querySelector('.map-search-input');
    const searchClear = searchContainer.querySelector('.map-search-clear');
    const searchResults = searchContainer.querySelector('.map-search-results');
    const searchModeButtons = searchContainer.querySelectorAll('.map-search-mode');

    let searchItems = [];
    let searchActiveIndex = -1;
    let searchDebounceTimer = null;
    let searchAbortController = null;
    let lastSearchQuery = '';
    let searchMode = 'address';

    function closeSearchResults() {
        searchResults.style.display = 'none';
        searchResults.innerHTML = '';
        searchItems = [];
        searchActiveIndex = -1;
    }

    function openSearchResults() {
        searchResults.style.display = 'block';
    }

    function renderSearchResults() {
        if (!searchItems.length) {
            searchResults.innerHTML = '<div class="map-search-empty">No matching Georgia parcels found.</div>';
            openSearchResults();
            return;
        }

        searchResults.innerHTML = searchItems.map((item, idx) => `
            <button class="map-search-item ${idx === searchActiveIndex ? 'active' : ''}" data-idx="${idx}">
                <div class="map-search-line1">${
                    searchMode === 'owner'
                        ? escapeHtml(item.owner_name || 'Unknown Owner')
                        : escapeHtml(item.site_address || 'Unknown Address')
                }</div>
                <div class="map-search-line2">${
                    searchMode === 'owner'
                        ? `${escapeHtml(item.site_address || 'Unknown Address')} · ${escapeHtml(item.county_name || 'Unknown County')} County, Georgia`
                        : `${escapeHtml(item.county_name || 'Unknown County')} County, Georgia`
                }</div>
            </button>
        `).join('');
        openSearchResults();
    }

    function flyToSearchResult(item) {
        if (!item) return;
        clearActivePopup();
        clearMapSelection();
        stopSearchFlyTrackingConflicts();

        if (item.feature_id) {
            selectedFeatureId = item.feature_id;
        }

        map.flyTo({
            center: [item.lng, item.lat],
            zoom: Math.max(map.getZoom(), 18),
            speed: 1.2,
            curve: 1.4
        });
        if (item.feature_id) {
            map.once('moveend', () => {
                map.setFeatureState(
                    { source: 'parcels', sourceLayer: 'parcels', id: item.feature_id },
                    { selected: true }
                );
                openParcelPopupFromSearch(item);
            });
        }
        searchInput.value = item.site_address || '';
        closeSearchResults();
    }

    async function runAddressSearch(query) {
        const trimmed = query.trim();
        if (trimmed.length < 2) {
            lastSearchQuery = '';
            closeSearchResults();
            return;
        }
        const normalized = trimmed.toLowerCase().replace(/\s+/g, ' ');
        if (normalized === lastSearchQuery) {
            return;
        }
        lastSearchQuery = normalized;

        if (searchAbortController) {
            searchAbortController.abort();
        }
        searchAbortController = new AbortController();

        try {
            const response = await fetch(`/api/search/parcels?q=${encodeURIComponent(trimmed)}&limit=10&mode=${encodeURIComponent(searchMode)}`, {
                method: 'GET',
                signal: searchAbortController.signal
            });
            if (!response.ok) {
                throw new Error(`Search failed (${response.status})`);
            }

            const payload = await response.json();
            searchItems = Array.isArray(payload.results) ? payload.results : [];
            searchActiveIndex = searchItems.length ? 0 : -1;
            renderSearchResults();
        } catch (err) {
            if (err.name === 'AbortError') return;
            searchItems = [];
            searchActiveIndex = -1;
            searchResults.innerHTML = '<div class="map-search-empty">Search unavailable right now.</div>';
            openSearchResults();
        }
    }

    searchInput.addEventListener('input', () => {
        if (searchDebounceTimer) {
            clearTimeout(searchDebounceTimer);
        }
        searchDebounceTimer = setTimeout(() => {
            runAddressSearch(searchInput.value);
        }, 280);
    });

    searchModeButtons.forEach((btn) => {
        btn.addEventListener('click', () => {
            const nextMode = btn.dataset.mode;
            if (nextMode === searchMode) return;
            searchMode = nextMode;
            searchInput.placeholder = searchMode === 'owner' ? 'Search Georgia owner...' : 'Search Georgia address...';
            lastSearchQuery = '';
            searchModeButtons.forEach((el) => {
                el.classList.toggle('active', el.dataset.mode === searchMode);
            });
            runAddressSearch(searchInput.value);
        });
    });

    searchInput.addEventListener('keydown', (e) => {
        if (!searchItems.length) {
            if (e.key === 'Enter') {
                e.preventDefault();
                runAddressSearch(searchInput.value);
            }
            if (e.key === 'Escape') {
                closeSearchResults();
            }
            return;
        }

        if (e.key === 'ArrowDown') {
            e.preventDefault();
            searchActiveIndex = (searchActiveIndex + 1) % searchItems.length;
            renderSearchResults();
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            searchActiveIndex = (searchActiveIndex - 1 + searchItems.length) % searchItems.length;
            renderSearchResults();
        } else if (e.key === 'Enter') {
            e.preventDefault();
            const idx = searchActiveIndex >= 0 ? searchActiveIndex : 0;
            flyToSearchResult(searchItems[idx]);
        } else if (e.key === 'Escape') {
            e.preventDefault();
            closeSearchResults();
        }
    });

    searchResults.addEventListener('click', (e) => {
        const button = e.target.closest('.map-search-item');
        if (!button) return;
        const idx = Number(button.dataset.idx);
        if (Number.isNaN(idx) || idx < 0 || idx >= searchItems.length) return;
        flyToSearchResult(searchItems[idx]);
    });

    searchClear.addEventListener('click', () => {
        searchInput.value = '';
        lastSearchQuery = '';
        closeSearchResults();
        searchInput.focus();
    });

    document.addEventListener('click', (e) => {
        if (!searchContainer.contains(e.target)) {
            closeSearchResults();
        }
    });

    // Keep label contrast appropriate for each basemap family.
    function applyOverlayTheme(styleId) {
        const darkTheme = styleId === 'dark' || styleId === 'black';
        const countyTextColor = darkTheme ? '#e9f0f6' : '#1f2a33';
        const countyHaloColor = darkTheme ? 'rgba(10, 16, 24, 0.95)' : 'rgba(255,255,255,0.9)';
        const parcelTextColor = darkTheme ? '#edf3f8' : '#23313d';
        const parcelHaloColor = darkTheme ? 'rgba(10, 16, 24, 0.95)' : '#ffffff';

        map.setPaintProperty('county-labels', 'text-color', countyTextColor);
        map.setPaintProperty('county-labels', 'text-halo-color', countyHaloColor);
        map.setPaintProperty('county-labels', 'text-halo-width', darkTheme ? 1.4 : 1.1);
        map.setPaintProperty('county-labels', 'text-halo-blur', darkTheme ? 0.45 : 0.35);

        map.setPaintProperty('parcel-labels', 'text-color', parcelTextColor);
        map.setPaintProperty('parcel-labels', 'text-halo-color', parcelHaloColor);
        map.setPaintProperty('parcel-labels', 'text-halo-width', darkTheme ? 1.4 : 1.2);
        map.setPaintProperty('parcel-labels', 'text-halo-blur', darkTheme ? 0.4 : 0.3);
        document.body.classList.toggle('map-theme-dark', darkTheme);
    }

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
        applyOverlayTheme(styleId);

        // Update UI to reflect current selection
        document.querySelectorAll('.style-option').forEach(el => {
            el.classList.toggle('active', el.dataset.style === styleId);
        });
    }

    map.on('load', () => {
        applyOverlayTheme(currentStyle);
    });

    // Add navigation controls
    map.addControl(new maplibregl.NavigationControl(), 'top-right');

    // Add GeolocateControl for location tracking with custom positioning
    geolocateControl = new maplibregl.GeolocateControl({
        positionOptions: { enableHighAccuracy: true },
        trackUserLocation: true,
        showUserLocation: true,
        showAccuracyCircle: true,
        showUserHeading: true  // Show heading arrow on user dot
    });
    map.addControl(geolocateControl, 'top-right');

    // Wire up compass rotation to the GeolocateControl.
    // Replaces the default fitBounds()-based camera update with direct transform
    // calls so compass bearing updates don't cancel zoom/pan gestures.
    setupCompassTracking(map, geolocateControl);

    // Auto-trigger geolocate if we detected user location on init.
    if (userLocation.accuracy) {
        map.once('load', () => {
            geolocateControl.trigger();
        });
    }

    async function fetchParcelDetails(featureId) {
        if (parcelDetailsCache.has(featureId)) {
            return parcelDetailsCache.get(featureId);
        }

        const response = await fetch(`/api/parcels/${encodeURIComponent(featureId)}`);
        if (!response.ok) {
            throw new Error(`Parcel lookup failed (${response.status})`);
        }

        const details = await response.json();
        parcelDetailsCache.set(featureId, details);
        return details;
    }

    async function openParcelPopupFromSearch(item) {
        if (!item || !item.feature_id || item.lat === undefined || item.lng === undefined) {
            return;
        }

        const tileProps = {
            feature_id: item.feature_id,
            site_address: item.site_address || '',
            acres: null,
            lat: item.lat,
            lng: item.lng
        };

        const popup = new maplibregl.Popup()
            .setLngLat([item.lng, item.lat])
            .setHTML(buildParcelLoadingPopup(tileProps))
            .addTo(map);
        setActivePopup(popup);

        try {
            const details = await fetchParcelDetails(item.feature_id);
            if (selectedFeatureId !== item.feature_id || !popup.isOpen()) {
                return;
            }
            popup.setHTML(buildParcelDetailsPopup(tileProps, details));
        } catch (err) {
            if (selectedFeatureId !== item.feature_id || !popup.isOpen()) {
                return;
            }
            popup.setHTML(createPopupHTML({
                currentStyle,
                title: tileProps.site_address || 'Parcel',
                accent: '#2f6ea9',
                maxWidth: 360,
                rows: [
                    { label: 'Address', value: tileProps.site_address || 'N/A' },
                    { label: 'Acres', value: 'N/A' },
                    { label: 'Details', value: 'Unable to load full parcel details' }
                ]
            }));
        }
    }

    function buildParcelLoadingPopup(props) {
        return createPopupHTML({
            currentStyle,
            title: props.site_address || 'Parcel',
            accent: '#2f6ea9',
            maxWidth: 360,
            rows: [
                { label: 'Address', value: props.site_address || 'N/A' },
                { label: 'Acres', value: formatNumber(props.acres) },
                { label: 'Owner', value: '<span class="popup-skeleton-line popup-skeleton-long"></span>' },
                { label: 'Class', value: '<span class="popup-skeleton-line popup-skeleton-medium"></span>' },
                { label: 'Tax Dist', value: '<span class="popup-skeleton-line popup-skeleton-short"></span>' }
            ],
            footerText: 'Loading full parcel details...'
        });
    }

    function buildParcelDetailsPopup(tileProps, details) {
        const siteAddress = details.site_address || tileProps.site_address || 'N/A';
        const acres = details.acres !== null && details.acres !== undefined ? details.acres : tileProps.acres;
        const classValue = `${details.category || 'N/A'} (${details.classification || 'N/A'})`;
        const lat = details.lat !== null && details.lat !== undefined ? details.lat : tileProps.lat;
        const lng = details.lng !== null && details.lng !== undefined ? details.lng : tileProps.lng;

        return createPopupHTML({
            currentStyle,
            title: siteAddress,
            accent: '#2f6ea9',
            maxWidth: 360,
            rows: [
                { label: 'Parcel ID', value: details.parcel_id || 'N/A' },
                { label: 'Address', value: siteAddress },
                { label: 'Owner', value: details.owner_name || 'N/A' },
                { label: 'Owner Addr', value: details.owner_address || 'N/A' },
                { label: 'Acres', value: formatNumber(acres) },
                { label: 'Class', value: classValue },
                { label: 'Tax Dist', value: details.tax_district || 'N/A' }
            ],
            footerHtml: buildParcelFooter({
                currentStyle,
                lat,
                lng,
                updatedAt: details.updated_at
            })
        });
    }

    // Click handler for parcels
    map.on('click', 'parcel-fill', async (e) => {
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

        const props = feature.properties;
        const tileProps = {
            ...props,
            lat: e.lngLat.lat,
            lng: e.lngLat.lng
        };
        const featureId = props.feature_id;
        const popup = new maplibregl.Popup()
            .setLngLat(e.lngLat)
            .setHTML(buildParcelLoadingPopup(tileProps))
            .addTo(map);
        setActivePopup(popup);

        try {
            const details = await fetchParcelDetails(featureId);

            // Ignore stale async responses if the user selected another parcel or closed popup.
            if (selectedFeatureId !== featureId || !popup.isOpen()) {
                return;
            }

            popup.setHTML(buildParcelDetailsPopup(tileProps, details));
        } catch (err) {
            if (selectedFeatureId !== featureId || !popup.isOpen()) {
                return;
            }

            popup.setHTML(createPopupHTML({
                currentStyle,
                title: tileProps.site_address || 'Parcel',
                accent: '#2f6ea9',
                maxWidth: 360,
                rows: [
                    { label: 'Address', value: tileProps.site_address || 'N/A' },
                    { label: 'Acres', value: formatNumber(tileProps.acres) },
                    { label: 'Details', value: 'Unable to load full parcel details' }
                ]
            }));
        }
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
        const popupHTML = createPopupHTML({
            currentStyle,
            title: `${props.name} County, ${props.state || 'GA'}`,
            accent: '#5f88a6',
            maxWidth: 280,
            rows: [
                { label: 'Population', value: formatNumber(props.population) },
                { label: 'Region', value: props.region || 'N/A' },
                { label: 'Acres', value: formatNumber(props.acres) },
                { label: 'Sq. Miles', value: formatNumber(props.square_miles) }
            ]
        });
        const popup = new maplibregl.Popup().setLngLat(e.lngLat).setHTML(popupHTML).addTo(map);
        setActivePopup(popup);
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

        if (parcelFeatures.length === 0 && countyFeatures.length === 0) {
            clearActivePopup();
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
