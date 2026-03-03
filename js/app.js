// MapLibre Vector Tiles
import {
    copyTextToClipboard,
    formatCurrency,
    formatLocalDateTime,
    formatNumber,
    toNumberOrNull
} from "./utils/format.js";
import {
    fetchCountyCentroidById,
    fetchParcelDetailsByFeatureId,
    fetchParcelTaxHistoryByFeatureId,
    fetchStyle,
} from "./api.js";
import { initSearchFeature } from "./features/search.js";
import { initOwnerResultsFeature } from "./features/ownerResults.js";
import {
    buildTaxHeatmapLayers,
    buildTaxHeatmapSources,
    initTaxHeatmapFeature,
    initTaxHeatmapPanelFeature,
    TAX_HEATMAP_COUNTY_ID,
    TAX_HEATMAP_DEFAULT_YEAR,
    TAX_HEATMAP_MAX_YEAR,
    TAX_HEATMAP_MIN_YEAR,
} from "./features/taxHeatmap.js";
import { initLocationControlsFeature } from "./features/locationControls.js";
import {
    initBasemapStyleFeature,
    initStyleSwitcherFeature,
    splitBasemapLayers,
    ensurePopupBaseStyles,
    ensureSearchStyles,
    ensureTaxHeatmapStyles,
    ensureOwnerResultsStyles,
} from "./mapStyles.js";
import {
    initCountyPopupFeature,
    initParcelPopupFeature,
    buildAddressValueHtml,
    buildOwnerAddressCopyText,
    buildSiteAddressCopyText,
    createPopupHTML,
    flashCopyButton,
} from "./popup.js";

const GEORGIA_CENTER = [-83.6, 32.8]; // [lng, lat]
const INITIAL_ZOOM = 7;

// Available basemap styles
const BASEMAP_STYLES = [
    { id: 'light', name: 'Light', icon: '\u2600\uFE0F' },
    { id: 'dark', name: 'Dark', icon: '\uD83C\uDF19' },
    { id: 'black', name: 'Black', icon: '\u2B1B' },
    { id: 'white', name: 'White', icon: '\u2B1C' },
    { id: 'grayscale', name: 'Grayscale', icon: '\uD83D\uDD18' },
    { id: 'satellite', name: 'Satellite', icon: '\uD83D\uDEF0\uFE0F' }
];

function createMapState(initialStyle = "light") {
    return {
        currentStyle: initialStyle,
    };
}

function createMapCaches() {
    return {
        parcelDetailsCache: new Map(),
        parcelTaxHistoryCache: new Map(),
        countyCentroidCache: new Map(),
        taxFeatureStateCacheByYear: new Map(),
        taxHeatmapStatsCache: new Map(),
        ownerResultsCache: new Map(),
        ownerPreviewCache: new Map(),
        ownerPreviewInFlight: new Set(),
        ownerPreviewTargets: new Map(),
        ownerPreviewPendingIDs: new Set(),
    };
}

const mapState = createMapState(localStorage.getItem('mapStyle') || 'light');
let currentStyle = mapState.currentStyle; // Load saved style or default

// Setup PMTiles Protocol
const protocol = new pmtiles.Protocol();
maplibregl.addProtocol('pmtiles', protocol.tile);


function isMobile() {
    return window.innerWidth < 768;
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

// Initializes map, feature modules, controls, and interaction wiring.
async function loadMap() {
    ensurePopupBaseStyles();
    ensureSearchStyles();
    ensureOwnerResultsStyles();
    ensureTaxHeatmapStyles();
    const initialSpriteStyleId = currentStyle === 'satellite' ? 'light' : currentStyle;

    const data = await fetchStyle(currentStyle);

    const basemapSplit = splitBasemapLayers(data.layers || []);
    const basemapBaseLayers = basemapSplit.base;
    const basemapLabelLayers = basemapSplit.labels;
    const initialBasemapLabelLayerIds = basemapLabelLayers.map((layer) => layer.id);
    const parcelFillColorExpression = ['case', ['boolean', ['feature-state', 'selected'], false], '#e4572e', '#2f6ea9'];
    const parcelFillOpacityExpression = ['interpolate', ['linear'], ['zoom'],
        13, ['case', ['boolean', ['feature-state', 'selected'], false], 0.42, 0.03],
        16, ['case', ['boolean', ['feature-state', 'selected'], false], 0.42, 0.06],
        19, ['case', ['boolean', ['feature-state', 'selected'], false], 0.42, 0.09]
    ];

    // Parcel Layers
    const parcelLayers = [
        {
            id: 'parcel-fill',
            type: 'fill',
            source: 'parcels',
            'source-layer': 'parcels',
            minzoom: 13,
            paint: {
                'fill-color': parcelFillColorExpression,
                'fill-opacity': parcelFillOpacityExpression
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
            id: 'parcel-outline-heatmap',
            type: 'line',
            source: 'parcels',
            'source-layer': 'parcels',
            minzoom: 13,
            layout: {
                visibility: 'none'
            },
            paint: {
                'line-color': '#3a4451',
                'line-width': ['interpolate', ['linear'], ['zoom'],
                    13, 0.45,
                    16, 0.75,
                    19, 1.0
                ],
                'line-opacity': ['interpolate', ['linear'], ['zoom'],
                    13, 0.25,
                    16, 0.40,
                    19, 0.55
                ]
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

    const heatmapLayers = buildTaxHeatmapLayers();

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
        attributionControl: false,
        dragRotate: false,
        pitchWithRotate: false,
        touchPitch: false,
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
            glyphs: '/assets/fonts/{fontstack}/{range}.pbf',
            sprite: `${window.location.origin}/assets/sprites/v4/${initialSpriteStyleId}`,
            sources: {
                'protomaps': {
                    type: 'vector',
                    url: `pmtiles:///georgia.pmtiles`,
                    attribution: '<a href="https://protomaps.com">Protomaps</a> &copy; <a href="https://openstreetmap.org">OpenStreetMap</a>'
                },
                'esri-imagery': {
                    type: 'raster',
                    tiles: ['https://services.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}'],
                    tileSize: 256,
                    minzoom: 0,
                    maxzoom: 19,
                    attribution: 'Tiles &copy; Esri'
                },
                'esri-reference': {
                    type: 'raster',
                    tiles: ['https://services.arcgisonline.com/ArcGIS/rest/services/Reference/World_Boundaries_and_Places/MapServer/tile/{z}/{y}/{x}'],
                    tileSize: 256,
                    minzoom: 0,
                    maxzoom: 19,
                    attribution: 'Labels &copy; Esri'
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
                },
                ...buildTaxHeatmapSources({
                    origin: window.location.origin,
                    defaultYear: TAX_HEATMAP_DEFAULT_YEAR,
                    countyId: TAX_HEATMAP_COUNTY_ID,
                }),
            },
            layers: [...basemapBaseLayers, ...countyLayers, ...heatmapLayers, ...parcelLayers, ...basemapLabelLayers]
        },
    });

    window.map = map; // Expose map for debugging

    // Track selected features for map highlighting.
    let selectedFeatureId = null;
    let selectedCountyId = null;
    let activePopup = null;
    const mapCaches = createMapCaches();
    const parcelDetailsCache = mapCaches.parcelDetailsCache;
    const parcelTaxHistoryCache = mapCaches.parcelTaxHistoryCache;
    const countyCentroidCache = mapCaches.countyCentroidCache;
    const taxHeatmapFeature = initTaxHeatmapFeature({
        map,
        mapCaches,
        parcelFillColorExpression,
        parcelFillOpacityExpression,
        taxHeatmapCountyID: TAX_HEATMAP_COUNTY_ID,
        defaultYear: TAX_HEATMAP_DEFAULT_YEAR
    });

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

    const PARCEL_POPUP_MOBILE_SCALE = 0.9;

    function centerParcelPopupOnMobile(popup) {
        if (!isMobile() || !popup || !popup.isOpen()) return;
        const popupEl = popup.getElement();
        if (!popupEl) return;

        const contentEl = popupEl.querySelector('.maplibregl-popup-content');
        const rect = (contentEl || popupEl).getBoundingClientRect();
        if (!rect.width || !rect.height) return;

        const containerRect = map.getContainer().getBoundingClientRect();
        const sidePad = 10;
        const topPad = 112;
        const bottomPad = 18;

        const desiredCenterX = containerRect.left + (containerRect.width / 2);
        const desiredCenterY = containerRect.top + ((containerRect.height + topPad - bottomPad) / 2);

        const minCenterX = containerRect.left + sidePad + (rect.width / 2);
        const maxCenterX = containerRect.right - sidePad - (rect.width / 2);
        const minCenterY = containerRect.top + topPad + (rect.height / 2);
        const maxCenterY = containerRect.bottom - bottomPad - (rect.height / 2);

        const targetCenterX = minCenterX <= maxCenterX
            ? Math.min(Math.max(desiredCenterX, minCenterX), maxCenterX)
            : (containerRect.left + containerRect.right) / 2;
        const targetCenterY = minCenterY <= maxCenterY
            ? Math.min(Math.max(desiredCenterY, minCenterY), maxCenterY)
            : (containerRect.top + containerRect.bottom) / 2;

        const currentCenterX = rect.left + (rect.width / 2);
        const currentCenterY = rect.top + (rect.height / 2);
        popup.setOffset([
            Math.round(targetCenterX - currentCenterX),
            Math.round(targetCenterY - currentCenterY)
        ]);
    }

    function recenterParcelPopup(popup) {
        if (!isMobile()) return;
        requestAnimationFrame(() => centerParcelPopupOnMobile(popup));
    }

    function createParcelPopupHTML(options) {
        return createPopupHTML({
            ...options,
            isMobile,
            mobileScale: PARCEL_POPUP_MOBILE_SCALE
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

    function clearCountySelection() {
        if (selectedCountyId !== null) {
            map.setFeatureState(
                { source: 'counties', sourceLayer: 'counties', id: selectedCountyId },
                { selected: false }
            );
            selectedCountyId = null;
        }
    }

    async function fetchCountyCentroid(countyID) {
        if (!countyID && countyID !== 0) return null;
        const key = String(countyID);
        if (countyCentroidCache.has(key)) {
            return countyCentroidCache.get(key);
        }

        const payload = await fetchCountyCentroidById(key);
        if (!Number.isFinite(payload?.lat) || !Number.isFinite(payload?.lng)) {
            throw new Error('County centroid payload missing lat/lng');
        }

        const value = { lat: payload.lat, lng: payload.lng };
        countyCentroidCache.set(key, value);
        return value;
    }

    const countyPopupFeature = initCountyPopupFeature({
        map,
        maplibregl,
        isMobile,
        getCurrentStyle: () => currentStyle,
        createPopupHTML,
        formatNumber,
        setActivePopup,
        clearCountySelection,        setSelectedCountyId: (countyId) => {
            selectedCountyId = countyId;
        },
        fetchCountyCentroid,
    });

    let styleSwitcherFeature = null;

    async function refreshActivePopupTheme() {
        if (!activePopup || !activePopup.isOpen() || !selectedFeatureId) return;
        try {
            const details = await fetchParcelDetails(selectedFeatureId);
            if (!activePopup || !activePopup.isOpen() || selectedFeatureId !== details.feature_id) return;

            const tileProps = {
                feature_id: selectedFeatureId,
                site_address: details.site_address || '',
                acres: details.acres,
                lat: details.lat,
                lng: details.lng
            };

            parcelPopupFeature.renderDetailsIntoPopup(activePopup, selectedFeatureId, tileProps, details);
        } catch (err) {
            console.warn('Failed to refresh popup theme:', err);
        }
    }

    const basemapStyleFeature = initBasemapStyleFeature({
        map,
        mapState,
        fetchStyle,
        initialLabelLayerIds: initialBasemapLabelLayerIds,
        getCurrentStyle: () => currentStyle,
        setCurrentStyle: (styleId) => {
            currentStyle = styleId;
        },
        customLayerIds: [...parcelLayers, ...countyLayers, ...heatmapLayers].map(l => l.id),
        anchorLayerId: countyLayers[0].id,
        onAfterStyleChange: (styleId) => {
            if (styleSwitcherFeature) {
                styleSwitcherFeature.refreshActiveStyle(styleId);
            }
            refreshActivePopupTheme();
        }
    });

    // Add navigation controls
    map.addControl(new maplibregl.NavigationControl(), 'top-right');
    map.addControl(new maplibregl.AttributionControl({ compact: true }), 'bottom-right');

    function enforceCompactAttribution() {
        const attrib = document.querySelector('.maplibregl-ctrl-attrib');
        if (!attrib) return;
        attrib.classList.add('maplibregl-compact');
        attrib.classList.remove('maplibregl-compact-show');
        const btn = attrib.querySelector('.maplibregl-ctrl-attrib-button');
        if (btn) btn.setAttribute('aria-expanded', 'false');
    }
    map.once('load', () => {
        enforceCompactAttribution();
        window.setTimeout(enforceCompactAttribution, 0);
        window.setTimeout(enforceCompactAttribution, 250);
    });
    map.on('resize', enforceCompactAttribution);

    const locationControlsFeature = initLocationControlsFeature({
        map,
        maplibregl,
        userLocation,
    });
    const stopSearchFlyTrackingConflicts = locationControlsFeature.stopSearchFlyTrackingConflicts;

    async function fetchParcelDetails(featureId) {
        if (parcelDetailsCache.has(featureId)) {
            return parcelDetailsCache.get(featureId);
        }
        const details = await fetchParcelDetailsByFeatureId(featureId);
        parcelDetailsCache.set(featureId, details);
        return details;
    }

    async function fetchParcelTaxHistory(featureId) {
        if (parcelTaxHistoryCache.has(featureId)) {
            return parcelTaxHistoryCache.get(featureId);
        }
        const payload = await fetchParcelTaxHistoryByFeatureId(featureId);
        const rows = Array.isArray(payload?.rows) ? payload.rows : [];
        parcelTaxHistoryCache.set(featureId, rows);
        return rows;
    }

    let openOwnerResultsForOwnerContext = null;

    const parcelPopupFeature = initParcelPopupFeature({
        map,
        maplibregl,
        isMobile,
        getCurrentStyle: () => currentStyle,
        createParcelPopupHTML,
        recenterParcelPopup,
        setActivePopup,
        getSelectedFeatureId: () => selectedFeatureId,
        setSelectedFeatureId: (featureId) => {
            selectedFeatureId = featureId;
        },
        fetchParcelDetails,
        fetchParcelTaxHistory,
        formatNumber,
        formatCurrency,
        formatLocalDateTime,
        toNumberOrNull,
        copyTextToClipboard,
        flashCopyButton,
        buildAddressValueHtml,
        buildSiteAddressCopyText,
        buildOwnerAddressCopyText,
        getOpenOwnerResultsForOwnerContext: () => openOwnerResultsForOwnerContext,
    });

    const ownerResultsFeature = initOwnerResultsFeature({
        map,
        isMobile,
        mapCaches,
        clearActivePopup,
        clearMapSelection,
        stopSearchFlyTrackingConflicts,
        openParcelPopupFromSearch: parcelPopupFeature.openParcelPopupFromSearch,
        setSelectedFeatureId: (featureId) => {
            selectedFeatureId = featureId;
        }
    });
    openOwnerResultsForOwnerContext = ownerResultsFeature.openOwnerResultsForOwnerContext;
    const closeOwnerResultsPane = ownerResultsFeature.closeOwnerResultsPane;

    initSearchFeature({
        map,
        closeOwnerResultsPane,
        clearActivePopup,
        clearMapSelection,
        stopSearchFlyTrackingConflicts,
        openParcelPopupFromSearch: parcelPopupFeature.openParcelPopupFromSearch,
        setSelectedFeatureId: (featureId) => {
            selectedFeatureId = featureId;
        }
    });

    styleSwitcherFeature = initStyleSwitcherFeature({
        styles: BASEMAP_STYLES,
        getCurrentStyle: () => currentStyle,
        onStyleSelected: (styleId) => {
            basemapStyleFeature.setMapStyle(styleId);
        }
    });

    // Register map events by concern so behavior is easy to scan and maintain.
    function registerMapLifecycleEvents() {
        map.on('load', () => {
            basemapStyleFeature.applyOverlayTheme(currentStyle);
            taxHeatmapFeature.updateTaxHeatmapSourceYear(taxHeatmapFeature.getPendingSourceYear());
        });
        map.on('moveend', () => {
            if (taxHeatmapFeature.isEnabled()) {
                taxHeatmapFeature.scheduleVisibleTaxFeatureHydration();
            }
            if (countyPopupFeature.hasActiveCountyPopup()) {
                countyPopupFeature.syncCountyPopupVisibility();
            }
        });
        map.on('move', () => {
            if (countyPopupFeature.hasActiveCountyPopup()) {
                countyPopupFeature.syncCountyPopupVisibility();
            }
        });
        map.on('zoomend', () => {
            if (countyPopupFeature.hasActiveCountyPopup()) {
                countyPopupFeature.syncCountyPopupVisibility();
            }
        });
    }

    function registerParcelInteractionEvents() {
        map.on('click', 'parcel-fill', async (e) => {
            if (e.features.length === 0) return;
            await parcelPopupFeature.handleParcelClick(e.features[0], e.lngLat);
        });

        map.on('mouseenter', 'parcel-fill', () => {
            map.getCanvas().style.cursor = 'pointer';
        });
        map.on('mouseleave', 'parcel-fill', () => {
            map.getCanvas().style.cursor = '';
        });

        map.on('click', (e) => {
            const parcelFeatures = map.queryRenderedFeatures(e.point, { layers: ['parcel-fill'] });
            if (parcelFeatures.length === 0 && selectedFeatureId !== null) {
                map.setFeatureState(
                    { source: 'parcels', sourceLayer: 'parcels', id: selectedFeatureId },
                    { selected: false }
                );
                selectedFeatureId = null;
            }

            const countyFeatures = map.queryRenderedFeatures(e.point, { layers: ['county-fill'] });
            if (countyFeatures.length === 0 && selectedCountyId !== null) {
                clearCountySelection();
            }

            if (parcelFeatures.length === 0 && countyFeatures.length === 0) {
                clearActivePopup();
            }
        });
    }

    function registerMapDiagnostics() {
        const zoomDisplay = document.createElement('div');
        zoomDisplay.style.cssText = `
            position: absolute;
            top: 226px;
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

        map.on('error', (e) => {
            if (e.error && e.error.message && e.error.message.includes('Unable to parse the tile')) {
                return;
            }
            console.error('Map error:', e);
        });
    }

    initTaxHeatmapPanelFeature({
        taxHeatmapFeature,
        isMobile,
        formatCurrency,
        defaultYear: TAX_HEATMAP_DEFAULT_YEAR,
        minYear: TAX_HEATMAP_MIN_YEAR,
        maxYear: TAX_HEATMAP_MAX_YEAR,
    });

    registerMapLifecycleEvents();
    registerParcelInteractionEvents();
    registerMapDiagnostics();
}

loadMap();