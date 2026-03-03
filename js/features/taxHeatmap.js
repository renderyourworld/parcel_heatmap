import { fetchTaxHeatmapStatsByYear } from "../api.js";

export const TAX_HEATMAP_DEFAULT_YEAR = 2024;
export const TAX_HEATMAP_MIN_YEAR = 2015;
export const TAX_HEATMAP_MAX_YEAR = 2024;
export const TAX_HEATMAP_COUNTY_ID = 671;

export function buildTaxHeatmapLayers() {
    return [
        {
            id: "tax-heatmap-county-bg",
            type: "fill",
            source: "counties",
            "source-layer": "counties",
            maxzoom: 9,
            layout: {
                visibility: "none",
            },
            paint: {
                "fill-color": "#355f8d",
                "fill-opacity": ["interpolate", ["linear"], ["zoom"], 5, 0.24, 8, 0.10, 9, 0.02],
            },
        },
        {
            id: "tax-heatmap-grid",
            type: "fill",
            source: "tax-heatmap",
            "source-layer": "tax_heatmap",
            minzoom: 6,
            maxzoom: 13,
            layout: {
                visibility: "none",
            },
            paint: {
                "fill-color": [
                    "interpolate",
                    ["linear"],
                    ["to-number", ["get", "avg_tax_amount"]],
                    0, "#1a9850",
                    1000, "#66bd63",
                    3000, "#fee08b",
                    6000, "#f46d43",
                    10000, "#d73027",
                ],
                "fill-opacity": ["interpolate", ["linear"], ["zoom"],
                    6, 0.45,
                    12, 0.58,
                    12.99, 0.58,
                    13, 0.0,
                ],
            },
        },
        {
            id: "tax-parcels-fill",
            type: "fill",
            source: "tax-parcels",
            "source-layer": "tax_parcels",
            minzoom: 13,
            maxzoom: 19,
            layout: {
                visibility: "none",
            },
            paint: {
                "fill-color": [
                    "interpolate",
                    ["linear"],
                    ["to-number", ["get", "tax_amount"]],
                    0, "#1a9850",
                    1000, "#66bd63",
                    3000, "#fee08b",
                    6000, "#f46d43",
                    10000, "#d73027",
                ],
                "fill-opacity": ["interpolate", ["linear"], ["zoom"],
                    13, 0.42,
                    16, 0.48,
                    19, 0.56,
                ],
            },
        },
    ];
}

export function buildTaxHeatmapSources({ origin, defaultYear, countyId }) {
    return {
        "tax-heatmap": {
            type: "vector",
            tiles: [`${origin}/api/tiles/tax-heatmap/{z}/{x}/{y}?year=${defaultYear}`],
            minzoom: 6,
            maxzoom: 16,
        },
        "tax-parcels": {
            type: "vector",
            tiles: [`${origin}/api/tiles/tax-parcels/{z}/{x}/{y}?year=${defaultYear}&county_id=${countyId}`],
            minzoom: 13,
            maxzoom: 19,
            promoteId: "feature_id",
        },
    };
}

export function initTaxHeatmapFeature({
    map,
    mapCaches,
    parcelFillOpacityExpression,
    taxHeatmapCountyID,
    defaultYear,
}) {
    const TAX_RANGE_SLIDER_MIN = 0;
    const TAX_RANGE_SLIDER_MAX = 1000;
    const taxHeatmapStatsCache = mapCaches.taxHeatmapStatsCache;

    let taxHeatmapEnabled = false;
    let taxHeatmapYear = defaultYear;
    let pendingHeatmapSourceYear = defaultYear;
    let taxRangeDomainMin = null;
    let taxRangeDomainMax = null;
    let taxRangeSelectedMin = null;
    let taxRangeSelectedMax = null;
    let taxRangeFilterTimer = null;
    let lastAppliedTaxRangeMin = null;
    let lastAppliedTaxRangeMax = null;

    function setLayerVisibility(layerId, visible) {
        if (!map.getLayer(layerId)) return;
        map.setLayoutProperty(layerId, "visibility", visible ? "visible" : "none");
    }

    function getHeatmapGridValueExpression() {
        return ["to-number", ["get", "avg_tax_amount"]];
    }

    function getTaxParcelsValueExpression() {
        return ["to-number", ["get", "tax_amount"]];
    }

    function buildHeatmapColorExpression(stats, valueExpression) {
        const minValue = Number(stats?.min_value ?? 0);
        const maxValue = Number(stats?.max_value ?? 0);
        const p10 = Number(stats?.p10 ?? minValue);
        const p50 = Number(stats?.p50 ?? ((minValue + maxValue) / 2));
        const p90 = Number(stats?.p90 ?? maxValue);
        if (!Number.isFinite(minValue) || !Number.isFinite(maxValue) || maxValue <= minValue) {
            return [
                "interpolate",
                ["linear"],
                valueExpression,
                0,
                "#1a9850",
                1000,
                "#66bd63",
                3000,
                "#fee08b",
                6000,
                "#f46d43",
                10000,
                "#d73027",
            ];
        }

        const low = Number.isFinite(p10) ? p10 : minValue;
        const mid = Number.isFinite(p50) ? p50 : (minValue + maxValue) / 2;
        const high = Number.isFinite(p90) ? p90 : maxValue;

        return [
            "interpolate",
            ["linear"],
            valueExpression,
            minValue,
            "#1a9850",
            low,
            "#66bd63",
            mid,
            "#fee08b",
            high,
            "#f46d43",
            maxValue,
            "#d73027",
        ];
    }

    function buildParcelSelectionOnlyOpacityExpression() {
        return [
            "interpolate",
            ["linear"],
            ["zoom"],
            13,
            ["case", ["boolean", ["feature-state", "selected"], false], 0.42, 0],
            16,
            ["case", ["boolean", ["feature-state", "selected"], false], 0.42, 0],
            19,
            ["case", ["boolean", ["feature-state", "selected"], false], 0.42, 0],
        ];
    }

    function clamp(value, min, max) {
        return Math.min(Math.max(value, min), max);
    }

    function taxRangeValueToSliderPosition(value) {
        if (!Number.isFinite(taxRangeDomainMin) || !Number.isFinite(taxRangeDomainMax)) {
            return TAX_RANGE_SLIDER_MIN;
        }
        const span = taxRangeDomainMax - taxRangeDomainMin;
        if (!Number.isFinite(span) || span <= 0) {
            return TAX_RANGE_SLIDER_MIN;
        }

        const relative = clamp(value - taxRangeDomainMin, 0, span);
        const normalized = Math.log1p(relative) / Math.log1p(span);
        return TAX_RANGE_SLIDER_MIN + normalized * (TAX_RANGE_SLIDER_MAX - TAX_RANGE_SLIDER_MIN);
    }

    function applyTaxRangeFilter(force = false) {
        if (!Number.isFinite(taxRangeSelectedMin) || !Number.isFinite(taxRangeSelectedMax)) return;
        if (!force && taxRangeSelectedMin === lastAppliedTaxRangeMin && taxRangeSelectedMax === lastAppliedTaxRangeMax) {
            return;
        }

        const gridFilter = [
            "all",
            [">=", getHeatmapGridValueExpression(), taxRangeSelectedMin],
            ["<=", getHeatmapGridValueExpression(), taxRangeSelectedMax],
        ];
        const parcelsFilter = [
            "all",
            [">=", getTaxParcelsValueExpression(), taxRangeSelectedMin],
            ["<=", getTaxParcelsValueExpression(), taxRangeSelectedMax],
        ];

        if (map.getLayer("tax-heatmap-grid")) {
            map.setFilter("tax-heatmap-grid", gridFilter);
        }
        if (map.getLayer("tax-parcels-fill")) {
            map.setFilter("tax-parcels-fill", parcelsFilter);
        }

        lastAppliedTaxRangeMin = taxRangeSelectedMin;
        lastAppliedTaxRangeMax = taxRangeSelectedMax;
    }

    function scheduleTaxRangeFilterApply() {
        if (taxRangeFilterTimer !== null) {
            clearTimeout(taxRangeFilterTimer);
        }
        taxRangeFilterTimer = setTimeout(() => {
            taxRangeFilterTimer = null;
            applyTaxRangeFilter();
        }, 45);
    }

    function updateTaxRangeVisualMask(dimLeftEl, dimRightEl) {
        if (!dimLeftEl || !dimRightEl) return;
        if (!Number.isFinite(taxRangeDomainMin) || !Number.isFinite(taxRangeDomainMax)) return;
        if (!Number.isFinite(taxRangeSelectedMin) || !Number.isFinite(taxRangeSelectedMax)) return;

        const span = taxRangeDomainMax - taxRangeDomainMin;
        if (!Number.isFinite(span) || span <= 0) {
            dimLeftEl.style.width = "0%";
            dimRightEl.style.width = "0%";
            return;
        }

        const clampedMin = Math.max(taxRangeDomainMin, Math.min(taxRangeDomainMax, taxRangeSelectedMin));
        const clampedMax = Math.max(taxRangeDomainMin, Math.min(taxRangeDomainMax, taxRangeSelectedMax));
        const minPos = taxRangeValueToSliderPosition(clampedMin);
        const maxPos = taxRangeValueToSliderPosition(clampedMax);
        const leftPct = ((minPos - TAX_RANGE_SLIDER_MIN) / (TAX_RANGE_SLIDER_MAX - TAX_RANGE_SLIDER_MIN)) * 100;
        const rightPct = ((TAX_RANGE_SLIDER_MAX - maxPos) / (TAX_RANGE_SLIDER_MAX - TAX_RANGE_SLIDER_MIN)) * 100;

        dimLeftEl.style.width = `${Math.max(0, Math.min(100, leftPct)).toFixed(4)}%`;
        dimRightEl.style.width = `${Math.max(0, Math.min(100, rightPct)).toFixed(4)}%`;
    }

    function setTaxHeatmapEnabled(enabled) {
        taxHeatmapEnabled = enabled;

        setLayerVisibility("tax-heatmap-grid", enabled);
        setLayerVisibility("tax-heatmap-county-bg", enabled);
        setLayerVisibility("tax-parcels-fill", enabled);
        setLayerVisibility("county-fill", !enabled);
        setLayerVisibility("county-labels", !enabled);

        setLayerVisibility("parcel-fill", true);
        setLayerVisibility("parcel-outline", true);
        setLayerVisibility("parcel-outline-heatmap", false);
        setLayerVisibility("parcel-labels", true);

        if (map.getLayer("parcel-fill")) {
            if (enabled) {
                map.setPaintProperty("parcel-fill", "fill-opacity", buildParcelSelectionOnlyOpacityExpression());
            } else {
                map.setPaintProperty("parcel-fill", "fill-opacity", parcelFillOpacityExpression);
            }
        }

        if (enabled) {
            const stats = taxHeatmapStatsCache.get(taxHeatmapYear);
            const gridColor = buildHeatmapColorExpression(stats, getHeatmapGridValueExpression());
            const parcelColor = buildHeatmapColorExpression(stats, getTaxParcelsValueExpression());
            if (map.getLayer("tax-heatmap-grid")) {
                map.setPaintProperty("tax-heatmap-grid", "fill-color", gridColor);
            }
            if (map.getLayer("tax-parcels-fill")) {
                map.setPaintProperty("tax-parcels-fill", "fill-color", parcelColor);
            }
            applyTaxRangeFilter(true);
        }
    }

    async function fetchTaxHeatmapStats(year) {
        if (taxHeatmapStatsCache.has(year)) {
            return taxHeatmapStatsCache.get(year);
        }
        const data = await fetchTaxHeatmapStatsByYear(taxHeatmapCountyID, year);
        taxHeatmapStatsCache.set(year, data);
        return data;
    }

    function setTaxHeatmapYear(year) {
        const parsedYear = Number(year);
        if (!Number.isFinite(parsedYear)) return;
        taxHeatmapYear = parsedYear;
    }

    function setTaxRangeDomain(minValue, maxValue) {
        const min = Number(minValue);
        const max = Number(maxValue);
        if (!Number.isFinite(min) || !Number.isFinite(max) || max < min) {
            return;
        }
        taxRangeDomainMin = min;
        taxRangeDomainMax = max;

        if (!Number.isFinite(taxRangeSelectedMin) || !Number.isFinite(taxRangeSelectedMax)) {
            taxRangeSelectedMin = min;
            taxRangeSelectedMax = max;
        } else {
            taxRangeSelectedMin = clamp(taxRangeSelectedMin, min, max);
            taxRangeSelectedMax = clamp(taxRangeSelectedMax, min, max);
            if (taxRangeSelectedMin > taxRangeSelectedMax) {
                taxRangeSelectedMin = min;
                taxRangeSelectedMax = max;
            }
        }
        lastAppliedTaxRangeMin = null;
        lastAppliedTaxRangeMax = null;
    }

    function setTaxRangeSelected(minValue, maxValue) {
        if (!Number.isFinite(taxRangeDomainMin) || !Number.isFinite(taxRangeDomainMax)) return;
        let min = clamp(Number(minValue), taxRangeDomainMin, taxRangeDomainMax);
        let max = clamp(Number(maxValue), taxRangeDomainMin, taxRangeDomainMax);
        if (!Number.isFinite(min) || !Number.isFinite(max)) return;
        if (min > max) {
            const tmp = min;
            min = max;
            max = tmp;
        }
        taxRangeSelectedMin = min;
        taxRangeSelectedMax = max;
    }

    function resetTaxRangeSelected() {
        if (!Number.isFinite(taxRangeDomainMin) || !Number.isFinite(taxRangeDomainMax)) return;
        taxRangeSelectedMin = taxRangeDomainMin;
        taxRangeSelectedMax = taxRangeDomainMax;
    }

    function getTaxRangeState() {
        return {
            domainMin: taxRangeDomainMin,
            domainMax: taxRangeDomainMax,
            selectedMin: taxRangeSelectedMin,
            selectedMax: taxRangeSelectedMax,
            year: taxHeatmapYear,
        };
    }

    function updateTaxHeatmapSourceYear(year) {
        pendingHeatmapSourceYear = year;
        if (!map.isStyleLoaded()) return;

        const gridSource = map.getSource("tax-heatmap");
        const parcelSource = map.getSource("tax-parcels");
        const gridURL = `${window.location.origin}/api/tiles/tax-heatmap/{z}/{x}/{y}?year=${year}`;
        const parcelURL = `${window.location.origin}/api/tiles/tax-parcels/{z}/{x}/{y}?year=${year}&county_id=${taxHeatmapCountyID}`;

        if (gridSource && typeof gridSource.setTiles === "function") {
            gridSource.setTiles([gridURL]);
        }
        if (parcelSource && typeof parcelSource.setTiles === "function") {
            parcelSource.setTiles([parcelURL]);
        }
    }

    // Maintained for app lifecycle compatibility; tile-driven mode needs no feature-state hydration.
    function scheduleVisibleTaxFeatureHydration() {}

    return {
        isEnabled: () => taxHeatmapEnabled,
        getPendingSourceYear: () => pendingHeatmapSourceYear,
        getTaxRangeState,
        scheduleVisibleTaxFeatureHydration,
        updateTaxHeatmapSourceYear,
        setTaxHeatmapEnabled,
        setTaxHeatmapYear,
        fetchTaxHeatmapStats,
        setTaxRangeDomain,
        setTaxRangeSelected,
        resetTaxRangeSelected,
        scheduleTaxRangeFilterApply,
        updateTaxRangeVisualMask,
    };
}

export function initTaxHeatmapPanelFeature({
    taxHeatmapFeature,
    isMobile,
    formatCurrency,
    defaultYear,
    minYear,
    maxYear,
}) {
    const existingBtn = document.querySelector(".tax-heatmap-tool-btn");
    if (existingBtn) existingBtn.remove();
    const existingPanel = document.querySelector(".tax-heatmap-panel");
    if (existingPanel) existingPanel.remove();

    const RANGE_SLIDER_MIN = 0;
    const RANGE_SLIDER_MAX = 1000;
    let uiBusy = false;
    let panelVisible = false;
    let panelDataLoaded = false;

    function sliderPositionToTaxValue(position, domainMin, domainMax) {
        const span = domainMax - domainMin;
        if (!Number.isFinite(span) || span <= 0) return domainMin;
        const normalized = (position - RANGE_SLIDER_MIN) / (RANGE_SLIDER_MAX - RANGE_SLIDER_MIN);
        return domainMin + (Math.expm1(normalized * Math.log1p(span)));
    }

    function taxValueToSliderPosition(value, domainMin, domainMax) {
        const span = domainMax - domainMin;
        if (!Number.isFinite(span) || span <= 0) return RANGE_SLIDER_MIN;
        const relative = Math.min(Math.max(value - domainMin, 0), span);
        const normalized = Math.log1p(relative) / Math.log1p(span);
        return RANGE_SLIDER_MIN + normalized * (RANGE_SLIDER_MAX - RANGE_SLIDER_MIN);
    }

    function safeRangeDomain(stats) {
        const min = Number(stats?.min_value);
        const max = Number(stats?.max_value);
        if (!Number.isFinite(min) || !Number.isFinite(max) || max < min) {
            return { min: 0, max: 10000 };
        }
        return { min, max };
    }

    const button = document.createElement("button");
    button.type = "button";
    button.className = "tax-heatmap-tool-btn";
    button.title = "Show/Hide Tax Heatmap Panel";
    button.setAttribute("aria-label", "Show/Hide Tax Heatmap Panel");
    button.setAttribute("aria-pressed", "false");
    button.innerHTML = `
        <svg width="15" height="15" viewBox="0 0 24 24" aria-hidden="true" style="display:block;">
            <path fill="currentColor" d="M13.5 2c.3 2.1-.3 3.6-1.8 5.1-1.1 1.1-1.7 2.1-1.7 3.4 0 1.5 1.2 2.7 2.7 2.7 2.1 0 3.8-1.8 3.8-4.1 0-1.9-.9-3.5-3-5.1zm-7 10.3c0 1.9.8 3.6 2.1 4.9 1.3 1.3 3.1 2.1 5.1 2.1s3.8-.8 5.1-2.1c1.3-1.3 2.1-3 2.1-4.9 0-2.5-1.4-4.8-3.6-6.2.7 1 .9 2 .9 3.1 0 3.3-2.5 6-5.6 6-2.8 0-5.1-2.2-5.1-5 0-.7.2-1.4.5-2-.9 1.1-1.5 2.6-1.5 4.1z"/>
        </svg>
    `;

    const panel = document.createElement("div");
    panel.className = "tax-heatmap-panel is-hidden";
    panel.innerHTML = `
        <div class="tax-heatmap-header">
            <strong>Tax Heatmap</strong>
            <label class="tax-heatmap-toggle">
                <input type="checkbox" data-role="enabled-toggle" />
                <span class="tax-heatmap-toggle-ui"><span class="tax-heatmap-toggle-knob"></span></span>
                <span class="tax-heatmap-toggle-text">On</span>
            </label>
        </div>
        <div class="tax-heatmap-year">Year: <strong data-role="year-label">${defaultYear}</strong></div>
        <input class="tax-heatmap-slider" data-role="year-slider" type="range" min="${minYear}" max="${maxYear}" step="1" value="${defaultYear}" />
        <div class="tax-heatmap-year-ticks" data-role="year-ticks"></div>
        <div class="tax-heatmap-year-scale"><span>${minYear}</span><span>${maxYear}</span></div>
        <div class="tax-heatmap-legend">
            <div class="tax-heatmap-year">Tax Range</div>
            <div class="tax-heatmap-range-controls">
                <div class="tax-heatmap-gradient"></div>
                <div class="tax-heatmap-dim tax-heatmap-dim-left" data-role="range-dim-left"></div>
                <div class="tax-heatmap-dim tax-heatmap-dim-right" data-role="range-dim-right"></div>
                <input class="tax-heatmap-range-slider" data-role="range-min-slider" type="range" min="${RANGE_SLIDER_MIN}" max="${RANGE_SLIDER_MAX}" step="1" value="${RANGE_SLIDER_MIN}" />
                <input class="tax-heatmap-range-slider" data-role="range-max-slider" type="range" min="${RANGE_SLIDER_MIN}" max="${RANGE_SLIDER_MAX}" step="1" value="${RANGE_SLIDER_MAX}" />
            </div>
            <div class="tax-heatmap-ticks">
                <span data-role="range-min-label">$0</span>
                <span data-role="range-max-label">$0</span>
            </div>
            <button type="button" class="tax-heatmap-reset" data-role="range-reset">Reset Range</button>
        </div>
    `;

    const enabledToggle = panel.querySelector("[data-role=\"enabled-toggle\"]");
    const yearLabel = panel.querySelector("[data-role=\"year-label\"]");
    const yearSlider = panel.querySelector("[data-role=\"year-slider\"]");
    const yearTicks = panel.querySelector("[data-role=\"year-ticks\"]");
    const rangeMinSlider = panel.querySelector("[data-role=\"range-min-slider\"]");
    const rangeMaxSlider = panel.querySelector("[data-role=\"range-max-slider\"]");
    const rangeMinLabel = panel.querySelector("[data-role=\"range-min-label\"]");
    const rangeMaxLabel = panel.querySelector("[data-role=\"range-max-label\"]");
    const rangeDimLeft = panel.querySelector("[data-role=\"range-dim-left\"]");
    const rangeDimRight = panel.querySelector("[data-role=\"range-dim-right\"]");
    const rangeReset = panel.querySelector("[data-role=\"range-reset\"]");

    const yearCount = maxYear - minYear + 1;
    for (let i = 0; i < yearCount; i++) {
        const tickYear = minYear + i;
        const tick = document.createElement("span");
        tick.className = "tax-heatmap-year-tick";
        tick.dataset.year = String(tickYear);
        yearTicks.appendChild(tick);
    }

    function updateActiveYearTick(year) {
        const ticks = yearTicks.querySelectorAll(".tax-heatmap-year-tick");
        ticks.forEach((tick) => {
            tick.classList.toggle("is-active", Number(tick.dataset.year) === year);
        });
    }

    function renderRangeUi() {
        const state = taxHeatmapFeature.getTaxRangeState();
        const domainMin = Number(state.domainMin);
        const domainMax = Number(state.domainMax);
        const selectedMin = Number(state.selectedMin);
        const selectedMax = Number(state.selectedMax);
        if (!Number.isFinite(domainMin) || !Number.isFinite(domainMax) || domainMax < domainMin) return;

        const minPos = taxValueToSliderPosition(selectedMin, domainMin, domainMax);
        const maxPos = taxValueToSliderPosition(selectedMax, domainMin, domainMax);

        rangeMinSlider.value = String(Math.round(minPos));
        rangeMaxSlider.value = String(Math.round(maxPos));
        rangeMinLabel.textContent = formatCurrency(selectedMin);
        rangeMaxLabel.textContent = formatCurrency(selectedMax);
        taxHeatmapFeature.updateTaxRangeVisualMask(rangeDimLeft, rangeDimRight);
    }

    function applyRangeFromSliders() {
        const state = taxHeatmapFeature.getTaxRangeState();
        const domainMin = Number(state.domainMin);
        const domainMax = Number(state.domainMax);
        if (!Number.isFinite(domainMin) || !Number.isFinite(domainMax) || domainMax < domainMin) return;

        let minPos = Number(rangeMinSlider.value);
        let maxPos = Number(rangeMaxSlider.value);
        if (!Number.isFinite(minPos) || !Number.isFinite(maxPos)) return;

        if (minPos > maxPos) {
            if (document.activeElement === rangeMinSlider) {
                maxPos = minPos;
                rangeMaxSlider.value = String(maxPos);
            } else {
                minPos = maxPos;
                rangeMinSlider.value = String(minPos);
            }
        }

        const selectedMin = sliderPositionToTaxValue(minPos, domainMin, domainMax);
        const selectedMax = sliderPositionToTaxValue(maxPos, domainMin, domainMax);
        taxHeatmapFeature.setTaxRangeSelected(selectedMin, selectedMax);
        renderRangeUi();
        taxHeatmapFeature.scheduleTaxRangeFilterApply();
    }

    async function loadYearAndSync(year) {
        const parsedYear = Number(year);
        if (!Number.isFinite(parsedYear)) return;
        yearLabel.textContent = String(parsedYear);
        updateActiveYearTick(parsedYear);
        taxHeatmapFeature.setTaxHeatmapYear(parsedYear);
        taxHeatmapFeature.updateTaxHeatmapSourceYear(parsedYear);

        const stats = await taxHeatmapFeature.fetchTaxHeatmapStats(parsedYear);
        const domain = safeRangeDomain(stats);
        taxHeatmapFeature.setTaxRangeDomain(domain.min, domain.max);
        taxHeatmapFeature.resetTaxRangeSelected();
        renderRangeUi();
        taxHeatmapFeature.scheduleTaxRangeFilterApply();

        if (taxHeatmapFeature.isEnabled()) {
            taxHeatmapFeature.setTaxHeatmapEnabled(true);
        }
    }

    async function ensurePanelDataLoaded() {
        if (panelDataLoaded) return;
        await loadYearAndSync(Number(yearSlider.value));
        panelDataLoaded = true;
    }

    function setPanelVisible(visible) {
        panelVisible = visible;
        panel.classList.toggle("is-hidden", !visible);
        button.classList.toggle("active", visible);
        button.setAttribute("aria-pressed", visible ? "true" : "false");
    }

    async function setEnabled(nextEnabled) {
        enabledToggle.checked = nextEnabled;

        if (nextEnabled) {
            await loadYearAndSync(Number(yearSlider.value));
            panelDataLoaded = true;
        }
        taxHeatmapFeature.setTaxHeatmapEnabled(nextEnabled);
    }

    enabledToggle.addEventListener("change", async () => {
        if (uiBusy) return;
        uiBusy = true;
        try {
            await setEnabled(enabledToggle.checked);
        } catch (err) {
            console.warn("Failed to toggle tax heatmap:", err);
        } finally {
            uiBusy = false;
        }
    });

    yearSlider.addEventListener("input", () => {
        const year = Number(yearSlider.value);
        yearLabel.textContent = String(year);
        updateActiveYearTick(year);
    });

    yearSlider.addEventListener("change", async () => {
        if (uiBusy) return;
        uiBusy = true;
        try {
            await loadYearAndSync(Number(yearSlider.value));
        } catch (err) {
            console.warn("Failed to update heatmap year:", err);
        } finally {
            uiBusy = false;
        }
    });

    rangeMinSlider.addEventListener("input", applyRangeFromSliders);
    rangeMaxSlider.addEventListener("input", applyRangeFromSliders);

    rangeReset.addEventListener("click", () => {
        taxHeatmapFeature.resetTaxRangeSelected();
        renderRangeUi();
        taxHeatmapFeature.scheduleTaxRangeFilterApply();
    });

    button.addEventListener("click", async () => {
        const nextVisible = !panelVisible;
        setPanelVisible(nextVisible);
        if (nextVisible) {
            try {
                await ensurePanelDataLoaded();
            } catch (err) {
                console.warn("Failed to load heatmap panel data:", err);
            }
        }
    });

    if (!isMobile()) {
        const header = panel.querySelector(".tax-heatmap-header");
        let dragging = false;
        let dragOffsetX = 0;
        let dragOffsetY = 0;

        const onMouseMove = (event) => {
            if (!dragging) return;
            panel.style.right = "auto";
            panel.style.left = `${Math.round(event.clientX - dragOffsetX)}px`;
            panel.style.top = `${Math.round(event.clientY - dragOffsetY)}px`;
        };

        const onMouseUp = () => {
            dragging = false;
            document.removeEventListener("mousemove", onMouseMove);
            document.removeEventListener("mouseup", onMouseUp);
        };

        header.addEventListener("mousedown", (event) => {
            if (event.button !== 0) return;
            const rect = panel.getBoundingClientRect();
            dragging = true;
            dragOffsetX = event.clientX - rect.left;
            dragOffsetY = event.clientY - rect.top;
            document.addEventListener("mousemove", onMouseMove);
            document.addEventListener("mouseup", onMouseUp);
        });
    }

    updateActiveYearTick(defaultYear);
    enabledToggle.checked = taxHeatmapFeature.isEnabled();
    setPanelVisible(false);
    document.body.appendChild(button);
    document.body.appendChild(panel);
}