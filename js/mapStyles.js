// Handles basemap layer switching and label/theme synchronization.
export function splitBasemapLayers(layers) {
    const base = [];
    const labels = [];
    for (const layer of layers) {
        const id = String(layer?.id || "");
        const isLabelLike = layer?.type === "symbol" || /label|reference/i.test(id);
        if (isLabelLike) {
            labels.push(layer);
        } else {
            base.push(layer);
        }
    }
    return { base, labels };
}

function tuneBasemapLabelLayers(map, labelLayerIds) {
    if (!map || !Array.isArray(labelLayerIds)) return;

    labelLayerIds.forEach((id) => {
        const layer = map.getLayer(id);
        if (!layer) return;

        if (layer.type === "symbol" && id.startsWith("places_")) {
            map.setLayoutProperty(id, "text-allow-overlap", false);
            map.setLayoutProperty(id, "text-ignore-placement", false);
            map.setLayoutProperty(id, "text-optional", true);
            if (id === "places_locality") {
                map.setLayerZoomRange(id, 9, 24);
            }
        }
    });

    if (map.getLayer("county-labels")) {
        map.moveLayer("county-labels");
    }
    if (map.getLayer("parcel-labels")) {
        map.moveLayer("parcel-labels");
    }
}

export function initBasemapStyleFeature({
    map,
    mapState,
    fetchStyle,
    initialLabelLayerIds,
    getCurrentStyle,
    setCurrentStyle,
    customLayerIds,
    anchorLayerId,
    onAfterStyleChange,
}) {
    let activeBasemapLabelLayerIds = Array.isArray(initialLabelLayerIds) ? initialLabelLayerIds.slice() : [];

    function applyOverlayTheme(styleId) {
        const darkTheme = styleId === "dark" || styleId === "black";
        const countyTextColor = darkTheme ? "#e9f0f6" : "#1f2a33";
        const countyHaloColor = darkTheme ? "rgba(10, 16, 24, 0.95)" : "rgba(255,255,255,0.9)";
        const parcelTextColor = darkTheme ? "#edf3f8" : "#23313d";
        const parcelHaloColor = darkTheme ? "rgba(10, 16, 24, 0.95)" : "#ffffff";

        map.setPaintProperty("county-labels", "text-color", countyTextColor);
        map.setPaintProperty("county-labels", "text-halo-color", countyHaloColor);
        map.setPaintProperty("county-labels", "text-halo-width", darkTheme ? 1.4 : 1.1);
        map.setPaintProperty("county-labels", "text-halo-blur", darkTheme ? 0.45 : 0.35);

        map.setPaintProperty("parcel-labels", "text-color", parcelTextColor);
        map.setPaintProperty("parcel-labels", "text-halo-color", parcelHaloColor);
        map.setPaintProperty("parcel-labels", "text-halo-width", darkTheme ? 1.4 : 1.2);
        map.setPaintProperty("parcel-labels", "text-halo-blur", darkTheme ? 0.4 : 0.3);
        document.body.classList.toggle("map-theme-dark", darkTheme);
        tuneBasemapLabelLayers(map, activeBasemapLabelLayerIds);
    }

    async function setMapStyle(styleId) {
        if (styleId === getCurrentStyle()) return;

        const data = await fetchStyle(styleId);
        const newBasemapSplit = splitBasemapLayers(data.layers || []);
        const newBasemapBaseLayers = newBasemapSplit.base;
        const newBasemapLabelLayers = newBasemapSplit.labels;

        const currentLayers = map.getStyle().layers;
        currentLayers.forEach((layer) => {
            if (!customLayerIds.includes(layer.id)) {
                map.removeLayer(layer.id);
            }
        });

        const spriteStyleId = styleId === "satellite" ? "light" : styleId;
        map.setSprite(`${window.location.origin}/assets/sprites/v4/${spriteStyleId}`);

        newBasemapBaseLayers.forEach((layer) => {
            map.addLayer(layer, anchorLayerId);
        });
        newBasemapLabelLayers.forEach((layer) => {
            map.addLayer(layer);
        });

        activeBasemapLabelLayerIds = newBasemapLabelLayers.map((layer) => layer.id);

        setCurrentStyle(styleId);
        mapState.currentStyle = styleId;
        localStorage.setItem("mapStyle", styleId);
        applyOverlayTheme(styleId);

        if (typeof onAfterStyleChange === "function") {
            onAfterStyleChange(styleId);
        }
    }

    return {
        applyOverlayTheme,
        setMapStyle,
    };
}



// Renders and manages the compact basemap style switcher control.
export function initStyleSwitcherFeature({
    styles,
    getCurrentStyle,
    onStyleSelected,
}) {
    const styleSwitcher = document.createElement("div");
    styleSwitcher.className = "style-switcher";
    styleSwitcher.style.cssText = `
        position: absolute;
        top: 188px;
        right: 10px;
        z-index: 1000;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    `;

    const mainButton = document.createElement("button");
    mainButton.className = "style-main-btn";
    mainButton.title = "Change map style";
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
    mainButton.innerHTML =
        '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#333" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 2 2 7 12 12 22 7 12 2"></polygon><polyline points="2 17 12 22 22 17"></polyline><polyline points="2 12 12 17 22 12"></polyline></svg>';

    mainButton.addEventListener("mouseenter", () => {
        mainButton.style.background = "#f4f4f4";
    });
    mainButton.addEventListener("mouseleave", () => {
        mainButton.style.background = "#fff";
    });

    const dropdown = document.createElement("div");
    dropdown.className = "style-dropdown";
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

    const optionsRow = document.createElement("div");
    optionsRow.style.cssText = "display: flex; padding: 4px;";

    styles.forEach((style) => {
        const option = document.createElement("button");
        option.className = "style-option";
        option.dataset.style = style.id;
        option.title = style.name;
        const isActive = style.id === getCurrentStyle();
        option.style.cssText = `
            width: 28px;
            height: 28px;
            border: none;
            border-radius: 3px;
            background: ${isActive ? "rgba(0, 123, 255, 0.15)" : "transparent"};
            cursor: pointer;
            font-size: 14px;
            transition: all 0.1s ease;
            display: flex;
            align-items: center;
            justify-content: center;
        `;
        option.textContent = style.icon;

        option.addEventListener("mouseenter", () => {
            if (style.id !== getCurrentStyle()) {
                option.style.background = "rgba(0, 0, 0, 0.05)";
            }
        });
        option.addEventListener("mouseleave", () => {
            option.style.background = style.id === getCurrentStyle() ? "rgba(0, 123, 255, 0.15)" : "transparent";
        });
        option.addEventListener("click", () => {
            onStyleSelected(style.id);
            dropdown.style.opacity = "0";
            dropdown.style.visibility = "hidden";
            dropdown.style.transform = "translateX(8px)";
        });

        optionsRow.appendChild(option);
    });

    function refreshActiveStyle(activeStyleId) {
        document.querySelectorAll(".style-option").forEach((el) => {
            const isActive = el.dataset.style === activeStyleId;
            el.classList.toggle("active", isActive);
            el.style.background = isActive ? "rgba(0, 123, 255, 0.15)" : "transparent";
        });
    }

    refreshActiveStyle(getCurrentStyle());

    dropdown.appendChild(optionsRow);

    mainButton.addEventListener("click", (e) => {
        e.stopPropagation();
        const isVisible = dropdown.style.visibility === "visible";
        dropdown.style.opacity = isVisible ? "0" : "1";
        dropdown.style.visibility = isVisible ? "hidden" : "visible";
        dropdown.style.transform = isVisible ? "translateX(8px)" : "translateX(0)";
    });

    document.addEventListener("click", () => {
        dropdown.style.opacity = "0";
        dropdown.style.visibility = "hidden";
        dropdown.style.transform = "translateX(8px)";
    });

    styleSwitcher.appendChild(mainButton);
    styleSwitcher.appendChild(dropdown);
    document.body.appendChild(styleSwitcher);

    return {
        refreshActiveStyle,
    };
}



export function ensurePopupBaseStyles() {
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

export function ensureSearchStyles() {
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
        .maplibregl-ctrl-attrib {
            max-width: calc(100vw - 16px);
        }
        .maplibregl-ctrl-attrib .maplibregl-ctrl-attrib-inner {
            display: none;
        }
        .maplibregl-ctrl-attrib.maplibregl-compact-show .maplibregl-ctrl-attrib-inner {
            display: block;
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
        .map-search-loading {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 12px;
            font-size: 13px;
            color: #5c6b7a;
        }
        .map-search-spinner {
            width: 12px;
            height: 12px;
            border-radius: 999px;
            border: 2px solid rgba(47, 110, 169, 0.25);
            border-top-color: #2f6ea9;
            animation: map-search-spin 0.7s linear infinite;
        }
        @keyframes map-search-spin {
            to { transform: rotate(360deg); }
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
        body.map-theme-dark .map-search-loading,
        body.map-theme-dark .map-search-clear {
            color: #b5c3d1;
        }
        body.map-theme-dark .map-search-spinner {
            border-color: rgba(79, 136, 191, 0.3);
            border-top-color: #67a7e4;
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

export function ensureTaxHeatmapStyles() {
    if (document.getElementById('tax-heatmap-styles')) return;

    const style = document.createElement('style');
    style.id = 'tax-heatmap-styles';
    style.textContent = `
        .tax-heatmap-panel {
            position: absolute;
            right: 50px;
            top: 112px;
            z-index: 1100;
            width: 320px;
            box-sizing: border-box;
            --tax-range-thumb-bg: #111;
            --tax-range-thumb-border: rgba(0, 0, 0, 0.35);
            border-radius: 12px;
            border: 1px solid rgba(0, 0, 0, 0.12);
            background: rgba(255, 255, 255, 0.96);
            box-shadow: 0 8px 20px rgba(0, 0, 0, 0.15);
            backdrop-filter: blur(2px);
            font-family: 'Noto Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
            padding: 10px 12px;
        }
        .tax-heatmap-panel.is-hidden {
            display: none;
        }
        .tax-heatmap-tool-btn {
            position: absolute;
            top: 150px;
            right: 10px;
            z-index: 1000;
            width: 29px;
            height: 29px;
            border: none;
            border-radius: 4px;
            background: #fff;
            box-shadow: 0 0 0 2px rgba(0,0,0,0.1);
            cursor: pointer;
            display: inline-flex;
            align-items: center;
            justify-content: center;
            color: #cc4b2f;
        }
        .tax-heatmap-tool-btn:hover {
            background: #f4f4f4;
        }
        .tax-heatmap-tool-btn.active {
            background: #ffe9dc;
            box-shadow: 0 0 0 2px rgba(204,75,47,0.28);
            color: #b9341a;
        }
        body.map-theme-dark .tax-heatmap-tool-btn {
            background: rgba(18, 24, 32, 0.96);
            box-shadow: 0 0 0 2px rgba(255,255,255,0.14);
            color: #ff8a63;
        }
        body.map-theme-dark .tax-heatmap-tool-btn:hover {
            background: rgba(36, 48, 64, 0.98);
        }
        body.map-theme-dark .tax-heatmap-tool-btn.active {
            background: rgba(255, 138, 99, 0.18);
            box-shadow: 0 0 0 2px rgba(255, 138, 99, 0.32);
            color: #ffb39a;
        }
        .tax-heatmap-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            font-size: 13px;
            color: #1f2a33;
            margin-bottom: 8px;
            cursor: move;
            user-select: none;
        }
        .tax-heatmap-toggle {
            display: inline-flex;
            align-items: center;
            gap: 6px;
            font-size: 12px;
            color: #1f2a33;
        }
        .tax-heatmap-toggle input {
            position: absolute;
            opacity: 0;
            width: 0;
            height: 0;
        }
        .tax-heatmap-toggle-ui {
            position: relative;
            width: 34px;
            height: 18px;
            border-radius: 999px;
            background: #c7d1db;
            border: 1px solid rgba(0, 0, 0, 0.14);
            transition: background 0.16s ease;
            flex-shrink: 0;
        }
        .tax-heatmap-toggle-knob {
            position: absolute;
            top: 1px;
            left: 1px;
            width: 14px;
            height: 14px;
            border-radius: 50%;
            background: #fff;
            box-shadow: 0 1px 2px rgba(0, 0, 0, 0.25);
            transition: transform 0.16s ease;
        }
        .tax-heatmap-toggle input:checked + .tax-heatmap-toggle-ui {
            background: #2f6ea9;
            border-color: rgba(47, 110, 169, 0.6);
        }
        .tax-heatmap-toggle input:checked + .tax-heatmap-toggle-ui .tax-heatmap-toggle-knob {
            transform: translateX(16px);
        }
        .tax-heatmap-toggle input:focus-visible + .tax-heatmap-toggle-ui {
            outline: 2px solid rgba(47, 110, 169, 0.35);
            outline-offset: 1px;
        }
        .tax-heatmap-toggle-text {
            font-size: 12px;
            color: inherit;
        }
        .tax-heatmap-year {
            margin-top: 6px;
            font-size: 12px;
            color: #495a6b;
        }
        .tax-heatmap-slider {
            width: 100%;
            margin-top: 6px;
        }
        .tax-heatmap-year-ticks {
            margin-top: 4px;
            display: flex;
            justify-content: space-between;
            align-items: center;
            height: 8px;
            padding: 0 1px;
        }
        .tax-heatmap-year-tick {
            width: 1px;
            height: 6px;
            background: rgba(80, 97, 112, 0.45);
        }
        .tax-heatmap-year-tick.is-active {
            height: 8px;
            background: #2f6ea9;
        }
        .tax-heatmap-year-scale {
            margin-top: 1px;
            display: flex;
            justify-content: space-between;
            font-size: 10px;
            color: #607181;
        }
        .tax-heatmap-legend {
            margin-top: 10px;
        }
        .tax-heatmap-range-controls {
            margin-top: 6px;
            position: relative;
            height: 34px;
        }
        .tax-heatmap-range-slider {
            position: absolute;
            left: 0;
            top: 0;
            z-index: 3;
            width: 100%;
            height: 34px;
            margin: 0;
            background: transparent;
            -webkit-appearance: none;
            appearance: none;
            pointer-events: none;
        }
        .tax-heatmap-range-slider::-webkit-slider-runnable-track {
            height: 34px;
            background: transparent;
        }
        .tax-heatmap-range-slider::-moz-range-track {
            height: 34px;
            background: transparent;
        }
        .tax-heatmap-range-slider::-webkit-slider-thumb {
            -webkit-appearance: none;
            appearance: none;
            width: 5px;
            height: 34px;
            border-radius: 0;
            border: 1px solid var(--tax-range-thumb-border);
            background: var(--tax-range-thumb-bg);
            cursor: ew-resize;
            pointer-events: auto;
            margin-top: 0;
        }
        .tax-heatmap-range-slider::-moz-range-thumb {
            width: 5px;
            height: 34px;
            border-radius: 0;
            border: 1px solid var(--tax-range-thumb-border);
            background: var(--tax-range-thumb-bg);
            cursor: ew-resize;
            pointer-events: auto;
        }
        .tax-heatmap-range-slider:focus {
            outline: none;
        }
        .tax-heatmap-gradient {
            position: absolute;
            left: 0;
            right: 0;
            top: 50%;
            z-index: 1;
            transform: translateY(-50%);
            height: 22px;
            border-radius: 0;
            background: linear-gradient(90deg, #1a9850 0%, #66bd63 25%, #fee08b 50%, #f46d43 75%, #d73027 100%);
            border: 1px solid rgba(0, 0, 0, 0.12);
        }
        .tax-heatmap-dim {
            position: absolute;
            top: 50%;
            z-index: 2;
            height: 22px;
            transform: translateY(-50%);
            background: rgba(255, 255, 255, 0.72);
            pointer-events: none;
        }
        .tax-heatmap-dim-left {
            left: 0;
            width: 0%;
        }
        .tax-heatmap-dim-right {
            right: 0;
            width: 0%;
        }
        .tax-heatmap-ticks {
            margin-top: 4px;
            display: flex;
            justify-content: space-between;
            font-size: 10px;
            color: #607181;
        }
        body.map-theme-dark .tax-heatmap-panel {
            --tax-range-thumb-bg: #e6ebef;
            --tax-range-thumb-border: rgba(255, 255, 255, 0.45);
            background: rgba(18, 24, 32, 0.96);
            border-color: rgba(255, 255, 255, 0.12);
        }
        body.map-theme-dark .tax-heatmap-header,
        body.map-theme-dark .tax-heatmap-toggle {
            color: #e7edf3;
        }
        body.map-theme-dark .tax-heatmap-year,
        body.map-theme-dark .tax-heatmap-year-scale,
        body.map-theme-dark .tax-heatmap-ticks {
            color: #b5c3d1;
        }
        body.map-theme-dark .tax-heatmap-year-tick {
            background: rgba(181, 195, 209, 0.55);
        }
        body.map-theme-dark .tax-heatmap-year-tick.is-active {
            background: #67a7e4;
        }
        body.map-theme-dark .tax-heatmap-toggle-ui {
            background: rgba(255, 255, 255, 0.22);
            border-color: rgba(255, 255, 255, 0.22);
        }
        body.map-theme-dark .tax-heatmap-toggle input:checked + .tax-heatmap-toggle-ui {
            background: #4f88bf;
            border-color: rgba(79, 136, 191, 0.75);
        }
        body.map-theme-dark .tax-heatmap-toggle-knob {
            background: #f3f7fb;
        }
        body.map-theme-dark .tax-heatmap-gradient {
            border-color: rgba(255, 255, 255, 0.14);
        }
        body.map-theme-dark .tax-heatmap-dim {
            background: rgba(0, 0, 0, 0.62);
        }
        .tax-heatmap-reset {
            margin-top: 8px;
            width: 100%;
            border: 1px solid rgba(0, 0, 0, 0.14);
            border-radius: 8px;
            background: rgba(255, 255, 255, 0.9);
            color: #2b3642;
            font-size: 12px;
            padding: 6px 10px;
            cursor: pointer;
        }
        .tax-heatmap-reset:hover {
            background: rgba(240, 244, 248, 0.95);
        }
        body.map-theme-dark .tax-heatmap-reset {
            border-color: rgba(255, 255, 255, 0.2);
            background: rgba(255, 255, 255, 0.05);
            color: #e7edf3;
        }
        @media (max-width: 767px) {
            .tax-heatmap-panel {
                width: auto;
                max-width: none;
                left: 10px;
                right: 10px;
                top: auto;
                bottom: 10px;
                max-height: 52vh;
                overflow-y: auto;
                -webkit-overflow-scrolling: touch;
            }
            .tax-heatmap-tool-btn {
                top: 150px;
            }
            .tax-heatmap-header {
                cursor: default;
            }
        }
    `;
    document.head.appendChild(style);
}

export function ensureOwnerResultsStyles() {
    if (document.getElementById('owner-results-styles')) return;

    const style = document.createElement('style');
    style.id = 'owner-results-styles';
    style.textContent = `
        .owner-results {
            position: absolute;
            top: 104px;
            left: 12px;
            bottom: 16px;
            z-index: 1085;
            width: 360px;
            align-items: stretch;
            pointer-events: none;
            transition: width 0.2s ease;
            font-family: 'Noto Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
        }
        .owner-results.is-collapsed {
            left: 0;
            width: 40px;
        }
        .owner-results-panel {
            pointer-events: auto;
            width: 100%;
            height: 100%;
            display: flex;
            flex-direction: column;
            background: rgba(255,255,255,0.97);
            border: 1px solid rgba(0,0,0,0.14);
            border-radius: 0 12px 12px 0;
            box-shadow: 0 8px 20px rgba(0,0,0,0.16);
            overflow: hidden;
            position: relative;
        }
        .owner-results.is-collapsed .owner-results-panel {
            display: none;
        }
        .owner-results-handle {
            position: absolute;
            left: calc(100% - 1px);
            top: 50%;
            transform: translateY(-50%);
            width: 24px;
            height: 86px;
            border: none;
            border-left: 1px solid rgba(0,0,0,0.12);
            border-top: 1px solid rgba(0,0,0,0.12);
            border-bottom: 1px solid rgba(0,0,0,0.12);
            border-radius: 0 10px 10px 0;
            background: rgba(246, 248, 251, 0.98);
            color: #2f4257;
            cursor: pointer;
            display: inline-flex;
            align-items: center;
            justify-content: center;
            z-index: 2;
            padding: 0;
            box-shadow: 0 2px 8px rgba(0,0,0,0.16);
            gap: 4px;
            pointer-events: auto;
        }
        .owner-results-handle-icon {
            display: inline-block;
            line-height: 1;
            transform: rotate(180deg);
            font-weight: 700;
        }
        .owner-results-handle-label {
            display: none;
            font-size: 10px;
            font-weight: 700;
            letter-spacing: 0.02em;
            writing-mode: vertical-rl;
            text-orientation: mixed;
            transform: rotate(180deg);
        }
        .owner-results.is-collapsed .owner-results-handle-icon {
            transform: none;
        }
        .owner-results.is-collapsed .owner-results-handle {
            left: 0;
            width: 28px;
            height: 84px;
            border-radius: 0 12px 12px 0;
            justify-content: center;
            padding: 0;
            flex-direction: row;
            box-shadow: 0 4px 12px rgba(0,0,0,0.22);
            border-left: none;
        }
        .owner-results.is-collapsed .owner-results-handle-label {
            display: none;
        }
        .owner-results.is-collapsed .owner-results-handle-icon {
            transform: none;
        }
        .owner-results-header {
            display: flex;
            align-items: flex-start;
            justify-content: space-between;
            padding: 10px 12px 8px 12px;
            border-bottom: 1px solid rgba(0,0,0,0.10);
            background: rgba(255,255,255,0.98);
        }
        .owner-results-heading {
            min-width: 0;
            flex: 1 1 auto;
        }
        .owner-results-close {
            display: none;
            border: none;
            background: transparent;
            color: #7a8898;
            width: 24px;
            height: 24px;
            border-radius: 6px;
            font-size: 20px;
            line-height: 1;
            cursor: pointer;
            padding: 0;
            margin-left: 8px;
            flex: 0 0 auto;
        }
        .owner-results-close:hover {
            background: rgba(82,97,112,0.1);
            color: #2f4257;
        }
        .owner-results-title {
            font-size: 14px;
            font-weight: 700;
            color: #1c2733;
            margin-bottom: 4px;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }
        .owner-results-subtitle {
            font-size: 12px;
            color: #5c6b7a;
        }
        .owner-results-body {
            flex: 1;
            overflow-y: auto;
            padding: 6px 0 6px 0;
        }
        .owner-results-empty,
        .owner-results-loading {
            padding: 12px;
            font-size: 13px;
            color: #5c6b7a;
        }
        .owner-results-loading {
            display: flex;
            align-items: center;
            gap: 8px;
        }
        .owner-results-item {
            display: block;
            width: 100%;
            box-sizing: border-box;
            border: none;
            background: transparent;
            text-align: left;
            padding: 10px 12px;
            border-bottom: 1px solid rgba(0,0,0,0.08);
            cursor: pointer;
        }
        .owner-results-item-layout {
            display: grid;
            grid-template-columns: minmax(0, 1fr) 76px;
            gap: 10px;
            align-items: center;
        }
        .owner-results-copy {
            min-width: 0;
        }
        .owner-results-item:hover {
            background: rgba(47, 110, 169, 0.10);
        }
        .owner-results-item.active {
            background: rgba(47, 110, 169, 0.16);
        }
        .owner-results-item.active .owner-results-preview {
            border-color: #2f6ea9;
            box-shadow: 0 0 0 2px rgba(47, 110, 169, 0.28);
        }
        .owner-results-line1 {
            font-size: 13px;
            color: #17222f;
            font-weight: 600;
            margin-bottom: 3px;
        }
        .owner-results-line2 {
            font-size: 12px;
            color: #526170;
            margin-bottom: 2px;
        }
        .owner-results-confidence {
            display: inline-flex;
            align-items: center;
            margin: 2px 0 3px 0;
            padding: 1px 7px;
            border-radius: 999px;
            font-size: 11px;
            font-weight: 700;
            line-height: 1.5;
            border: 1px solid transparent;
        }
        .owner-results-confidence.high {
            color: #16643a;
            background: rgba(31, 157, 85, 0.12);
            border-color: rgba(31, 157, 85, 0.35);
        }
        .owner-results-confidence.medium {
            color: #8a6400;
            background: rgba(212, 160, 23, 0.14);
            border-color: rgba(212, 160, 23, 0.35);
        }
        .owner-results-confidence.low {
            color: #a32a1f;
            background: rgba(207, 59, 46, 0.12);
            border-color: rgba(207, 59, 46, 0.34);
        }
        .owner-results-meta-row {
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: 8px;
        }
        .owner-results-meta-toggle {
            width: 18px;
            height: 18px;
            border-radius: 999px;
            border: 1px solid rgba(82,97,112,0.36);
            color: #526170;
            background: rgba(255,255,255,0.85);
            display: inline-flex;
            align-items: center;
            justify-content: center;
            font-size: 11px;
            font-weight: 700;
            line-height: 1;
            cursor: pointer;
            user-select: none;
            flex: 0 0 auto;
        }
        .owner-results-meta-toggle.is-open {
            border-color: rgba(47,110,169,0.55);
            color: #1f4f7a;
            background: rgba(47,110,169,0.14);
        }
        .owner-results-meta-panel {
            display: none;
            margin: 4px 0 5px 0;
            padding: 6px 8px;
            border-radius: 7px;
            border: 1px solid rgba(82,97,112,0.18);
            background: rgba(240,244,248,0.6);
            font-size: 11.5px;
            color: #4b5b6c;
            line-height: 1.35;
            gap: 2px;
        }
        .owner-results-meta-panel.is-open {
            display: grid;
        }
        .owner-results-meta-label {
            font-weight: 700;
            color: #3a4a5a;
        }
        .owner-results-line3 {
            font-size: 12px;
            color: #6a7887;
        }
        .owner-results-preview {
            width: 72px;
            height: 72px;
            border: 1px solid rgba(0,0,0,0.14);
            border-radius: 8px;
            background: rgba(255,255,255,0.82);
            display: grid;
            place-items: center;
            overflow: hidden;
            justify-self: end;
        }
        .owner-results-preview-placeholder {
            width: 38px;
            height: 38px;
            border-radius: 8px;
            background: linear-gradient(90deg, rgba(140,155,170,0.18) 25%, rgba(190,205,220,0.38) 50%, rgba(140,155,170,0.18) 75%);
            background-size: 200% 100%;
            animation: popupShimmer 1.2s ease-in-out infinite;
        }
        .owner-results-preview-empty {
            width: 38px;
            height: 38px;
            border-radius: 8px;
            border: 1px dashed rgba(127,142,160,0.35);
            opacity: 0.6;
        }
        .owner-results-preview-svg {
            width: 100%;
            height: 100%;
            display: block;
        }
        .owner-results-preview-path {
            fill: rgba(47, 110, 169, 0.18);
            stroke: rgba(47, 110, 169, 0.9);
            stroke-width: 1.3;
            vector-effect: non-scaling-stroke;
        }
        .owner-results-pagination {
            display: flex;
            align-items: center;
            gap: 6px;
            flex-wrap: wrap;
            justify-content: center;
            border-top: 1px solid rgba(0,0,0,0.10);
            padding: 8px 10px;
            background: rgba(255,255,255,0.98);
        }
        .owner-results-page-btn {
            border: 1px solid rgba(0,0,0,0.18);
            background: #fff;
            color: #2f3f4f;
            font-size: 12px;
            border-radius: 6px;
            padding: 4px 8px;
            cursor: pointer;
            min-width: 30px;
        }
        .owner-results-page-btn.active {
            background: rgba(47, 110, 169, 0.16);
            border-color: rgba(47, 110, 169, 0.45);
            color: #1f4f7a;
            font-weight: 700;
        }
        .owner-results-page-btn:disabled {
            opacity: 0.45;
            cursor: default;
        }
        body.map-theme-dark .owner-results-panel {
            background: rgba(18,24,32,0.96);
            border-color: rgba(255,255,255,0.14);
        }
        body.map-theme-dark .owner-results-handle {
            background: rgba(30, 42, 56, 0.96);
            color: #d7e3ef;
            border-right-color: rgba(255,255,255,0.14);
            border-left-color: rgba(255,255,255,0.14);
            border-top-color: rgba(255,255,255,0.14);
            border-bottom-color: rgba(255,255,255,0.14);
        }
        body.map-theme-dark .owner-results-title {
            color: #edf3f8;
        }
        body.map-theme-dark .owner-results-close {
            color: #b2c3d6;
        }
        body.map-theme-dark .owner-results-close:hover {
            background: rgba(172,190,208,0.16);
            color: #e6eff8;
        }
        body.map-theme-dark .owner-results-subtitle,
        body.map-theme-dark .owner-results-empty,
        body.map-theme-dark .owner-results-loading,
        body.map-theme-dark .owner-results-line2,
        body.map-theme-dark .owner-results-line3 {
            color: #b7c4d1;
        }
        body.map-theme-dark .owner-results-confidence.high {
            color: #9de4bd;
            background: rgba(31, 157, 85, 0.22);
            border-color: rgba(121, 217, 163, 0.48);
        }
        body.map-theme-dark .owner-results-confidence.medium {
            color: #f5d88a;
            background: rgba(212, 160, 23, 0.24);
            border-color: rgba(234, 196, 96, 0.50);
        }
        body.map-theme-dark .owner-results-confidence.low {
            color: #f4afa8;
            background: rgba(207, 59, 46, 0.22);
            border-color: rgba(230, 122, 111, 0.46);
        }
        body.map-theme-dark .owner-results-meta-toggle {
            border-color: rgba(172,190,208,0.42);
            color: #b7c4d1;
            background: rgba(18,24,32,0.85);
        }
        body.map-theme-dark .owner-results-meta-toggle.is-open {
            border-color: rgba(121, 181, 236, 0.62);
            color: #d6ebff;
            background: rgba(79, 136, 191, 0.25);
        }
        body.map-theme-dark .owner-results-meta-panel {
            border-color: rgba(172,190,208,0.24);
            background: rgba(33,45,58,0.75);
            color: #c6d4e1;
        }
        body.map-theme-dark .owner-results-meta-label {
            color: #e2edf7;
        }
        body.map-theme-dark .owner-results-line1 {
            color: #edf3f8;
        }
        body.map-theme-dark .owner-results-preview {
            background: rgba(24,34,46,0.72);
            border-color: rgba(255,255,255,0.16);
        }
        body.map-theme-dark .owner-results-preview-path {
            fill: rgba(103, 167, 228, 0.18);
            stroke: rgba(166, 214, 255, 0.95);
        }
        body.map-theme-dark .owner-results-item {
            border-bottom-color: rgba(255,255,255,0.09);
        }
        body.map-theme-dark .owner-results-item:hover {
            background: rgba(79, 136, 191, 0.16);
        }
        body.map-theme-dark .owner-results-item.active {
            background: rgba(79, 136, 191, 0.24);
        }
        body.map-theme-dark .owner-results-item.active .owner-results-preview {
            border-color: #67a7e4;
            box-shadow: 0 0 0 2px rgba(103, 167, 228, 0.30);
        }
        body.map-theme-dark .owner-results-pagination {
            border-top-color: rgba(255,255,255,0.12);
            background: rgba(18,24,32,0.98);
        }
        body.map-theme-dark .owner-results-page-btn {
            border-color: rgba(255,255,255,0.18);
            background: rgba(18,24,32,0.96);
            color: #d5e1ed;
        }
        body.map-theme-dark .owner-results-page-btn.active {
            background: rgba(79, 136, 191, 0.24);
            border-color: rgba(79, 136, 191, 0.55);
            color: #dcefff;
        }
        @media (max-width: 767px) {
            .owner-results {
                top: 118px;
                left: 10px;
                right: 10px;
                bottom: 12px;
                width: auto;
                z-index: 1120;
            }
            .owner-results .owner-results-panel {
                border-radius: 12px;
                background: rgba(255,255,255,0.995);
            }
            .owner-results-handle {
                top: auto;
                bottom: 14px;
                left: 50%;
                transform: translateX(-50%);
                width: 40px;
                height: 30px;
                border-radius: 999px;
                border: 1px solid rgba(0,0,0,0.12);
                border-left: 1px solid rgba(0,0,0,0.12);
                flex-direction: row;
                gap: 0;
                padding: 0;
            }
            .owner-results-handle-icon {
                transform: rotate(90deg);
            }
            .owner-results-handle-label {
                display: none;
                writing-mode: horizontal-tb;
                transform: none;
                font-size: 11px;
            }
            .owner-results-item-layout {
                grid-template-columns: minmax(0, 1fr) 56px;
                gap: 8px;
                align-items: center;
            }
            .owner-results-preview {
                display: grid;
                width: 56px;
                height: 56px;
            }
            .owner-results:not(.is-collapsed) .owner-results-handle {
                display: none;
            }
            .owner-results:not(.is-collapsed) .owner-results-close {
                display: inline-flex;
                align-items: center;
                justify-content: center;
            }
            .owner-results.is-collapsed {
                top: auto;
                left: 0;
                right: 0;
                bottom: 14px;
                width: auto;
                height: 30px;
                transform: none;
                pointer-events: none;
            }
            .owner-results.is-collapsed .owner-results-panel {
                display: none;
            }
            .owner-results.is-collapsed .owner-results-handle {
                pointer-events: auto;
                left: 50%;
                bottom: 0;
                top: auto;
                transform: translateX(-50%);
                width: 40px;
                height: 30px;
                border-radius: 999px;
                border: 1px solid rgba(0,0,0,0.12);
                border-left: 1px solid rgba(0,0,0,0.12);
                flex-direction: row;
                padding: 0;
                justify-content: center;
            }
            .owner-results.is-collapsed .owner-results-handle-icon {
                transform: rotate(-90deg);
            }
        }
    `;
    document.head.appendChild(style);
}