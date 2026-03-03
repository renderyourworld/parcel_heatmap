// Manages owner reverse-search results pane and interactions.
import { fetchOwnerProperties, fetchParcelPreviews } from "../api.js";
import { escapeHtml, formatNumber } from "../utils/format.js";

export function initOwnerResultsFeature({
    map,
    isMobile,
    mapCaches,
    clearActivePopup,
    clearMapSelection,
    stopSearchFlyTrackingConflicts,
    openParcelPopupFromSearch,
    setSelectedFeatureId,
}) {
    const OWNER_RESULTS_PAGE_SIZE = 25;

    const ownerResultsContainer = document.createElement("div");
    ownerResultsContainer.className = "owner-results is-collapsed";
    ownerResultsContainer.innerHTML = `
        <button class="owner-results-handle" type="button" aria-expanded="false" aria-label="Toggle owner results" title="Toggle owner results">
            <span class="owner-results-handle-icon" aria-hidden="true">
                <svg width="14" height="14" viewBox="0 0 16 16" style="display:block;">
                    <path d="M3 2l5 6-5 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                    <path d="M8 2l5 6-5 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                </svg>
            </span>
        </button>
        <section class="owner-results-panel" aria-label="Owner property results">
            <header class="owner-results-header">
                <div class="owner-results-heading">
                    <div class="owner-results-title">Results</div>
                    <div class="owner-results-subtitle">Select "Properties by this Owner" from a parcel.</div>
                </div>
                <button class="owner-results-close" type="button" aria-label="Close results" title="Close results">&times;</button>
            </header>
            <div class="owner-results-body">
                <div class="owner-results-empty">No owner results loaded.</div>
            </div>
            <div class="owner-results-pagination"></div>
        </section>
    `;
    document.body.appendChild(ownerResultsContainer);

    const ownerResultsToggle = ownerResultsContainer.querySelector(".owner-results-handle");
    const ownerResultsTitle = ownerResultsContainer.querySelector(".owner-results-title");
    const ownerResultsSubtitle = ownerResultsContainer.querySelector(".owner-results-subtitle");
    const ownerResultsClose = ownerResultsContainer.querySelector(".owner-results-close");
    const ownerResultsBody = ownerResultsContainer.querySelector(".owner-results-body");
    const ownerResultsPagination = ownerResultsContainer.querySelector(".owner-results-pagination");

    let ownerResultsAbortController = null;
    let ownerResultsLoading = false;
    let ownerResultsOwnerName = "";
    let ownerResultsOwnerAddress = "";
    let ownerResultsFeatureID = "";
    let ownerResultsMatchType = "";
    let ownerResultsPage = 1;
    let ownerResultsTotalPages = 0;
    let ownerResultsTotalProperties = 0;
    let ownerResultsCountyCount = 0;
    let ownerResultsRows = [];
    let ownerResultsSelectedFeatureID = null;
    let ownerResultsAllRows = [];
    let ownerResultsExpandedFeatureIDs = new Set();
    const ownerResultsCache = mapCaches.ownerResultsCache;
    const ownerPreviewCache = mapCaches.ownerPreviewCache;
    const ownerPreviewInFlight = mapCaches.ownerPreviewInFlight;
    const ownerPreviewTargets = mapCaches.ownerPreviewTargets;
    let ownerPreviewObserver = null;
    let ownerPreviewBatchTimer = null;
    const ownerPreviewPendingIDs = mapCaches.ownerPreviewPendingIDs;

    function setOwnerResultsPaneOpen(open) {
        ownerResultsContainer.classList.toggle("is-collapsed", !open);
        ownerResultsToggle.setAttribute("aria-expanded", open ? "true" : "false");
    }

    function openOwnerResultsPane() {
        setOwnerResultsPaneOpen(true);
    }

    function closeOwnerResultsPane() {
        setOwnerResultsPaneOpen(false);
    }

    ownerResultsToggle.addEventListener("click", () => {
        const isCollapsed = ownerResultsContainer.classList.contains("is-collapsed");
        setOwnerResultsPaneOpen(isCollapsed);
    });
    ownerResultsClose.addEventListener("click", () => {
        closeOwnerResultsPane();
    });

    function buildOwnerResultsPageButtons(current, total) {
        if (total <= 0) return [];
        const maxButtons = 7;
        if (total <= maxButtons) {
            return Array.from({ length: total }, (_, i) => i + 1);
        }
        const pages = new Set([1, total, current]);
        for (let delta = 1; delta <= 2; delta += 1) {
            if (current - delta > 1) pages.add(current - delta);
            if (current + delta < total) pages.add(current + delta);
        }
        return Array.from(pages).sort((a, b) => a - b);
    }

    function formatOwnerResultsClass(item) {
        const category = item?.category || "";
        const classification = item?.classification || "";
        if (category && classification) return `${category} (${classification})`;
        return classification || category || "N/A";
    }

    function renderOwnerResultsPane() {
        if (!ownerResultsOwnerName) {
            ownerResultsTitle.textContent = "Results";
            ownerResultsSubtitle.textContent = 'Select "Properties by this Owner" from a parcel.';
            ownerResultsBody.innerHTML = '<div class="owner-results-empty">No owner results loaded.</div>';
            ownerResultsPagination.innerHTML = "";
            return;
        }

        ownerResultsTitle.textContent = ownerResultsOwnerName;

        if (ownerResultsLoading) {
            ownerResultsSubtitle.textContent = "Loading owner properties...";
            ownerResultsBody.innerHTML = `
                <div class="owner-results-loading">
                    <span class="map-search-spinner"></span>
                    <span>Loading owner properties...</span>
                </div>
            `;
            ownerResultsPagination.innerHTML = "";
            return;
        }

        ownerResultsSubtitle.textContent = `${ownerResultsMatchType === "hybrid_scored" ? "Hybrid Match" : "Owner Match"}: ${formatNumber(ownerResultsTotalProperties)} properties across ${formatNumber(ownerResultsCountyCount)} counties`;

        if (!ownerResultsAllRows.length) {
            ownerResultsBody.innerHTML = '<div class="owner-results-empty">No properties found for this owner.</div>';
            ownerResultsPagination.innerHTML = "";
            return;
        }

        const start = (ownerResultsPage - 1) * OWNER_RESULTS_PAGE_SIZE;
        const end = start + OWNER_RESULTS_PAGE_SIZE;
        ownerResultsRows = ownerResultsAllRows.slice(start, end);

        ownerResultsBody.innerHTML = ownerResultsRows
            .map((item, idx) => {
                const address = item?.site_address || "Address unavailable";
                const county = item?.county_name || "Unknown County";
                const classText = formatOwnerResultsClass(item);
                const taxDist = item?.tax_district || "N/A";
                const acresText = item?.acres === null || item?.acres === undefined ? "N/A" : formatNumber(item.acres);
                const isActive = item?.feature_id && item.feature_id === ownerResultsSelectedFeatureID;
                const featureID = String(item?.feature_id || "");
                const isMetaOpen = featureID && ownerResultsExpandedFeatureIDs.has(featureID);
                const confidence = Number.isFinite(Number(item?.match_confidence)) ? Math.max(0, Math.min(100, Math.round(Number(item.match_confidence)))) : 0;
                const confidenceBand = (item?.match_band || "low").toLowerCase();
                const confidenceClass =
                    confidenceBand === "high"
                        ? "owner-results-confidence high"
                        : confidenceBand === "medium"
                          ? "owner-results-confidence medium"
                          : "owner-results-confidence low";
                const reasons = Array.isArray(item?.match_reasons) ? item.match_reasons.join(" | ") : "";
                const ownerNameText = (item?.owner_name || ownerResultsOwnerName || "N/A").trim() || "N/A";
                const ownerAddressText = (item?.owner_address || "N/A").trim() || "N/A";

                return `
                <button class="owner-results-item ${isActive ? "active" : ""}" data-idx="${idx}">
                    <div class="owner-results-item-layout">
                        <div class="owner-results-copy">
                            <div class="owner-results-line1">${escapeHtml(address)}</div>
                            <div class="owner-results-line2">${escapeHtml(county)} County</div>
                            <div class="owner-results-meta-row">
                                <div class="${confidenceClass}" title="${escapeHtml(reasons || "Confidence score")}">Match Confidence: ${confidence}%</div>
                                <span
                                    class="owner-results-meta-toggle ${isMetaOpen ? "is-open" : ""}"
                                    data-owner-meta-toggle="true"
                                    data-feature-id="${escapeHtml(featureID)}"
                                    title="Show owner details"
                                    role="button"
                                    tabindex="0"
                                    aria-expanded="${isMetaOpen ? "true" : "false"}"
                                >i</span>
                            </div>
                            <div class="owner-results-meta-panel ${isMetaOpen ? "is-open" : ""}">
                                <div><span class="owner-results-meta-label">Owner:</span> ${escapeHtml(ownerNameText)}</div>
                                <div><span class="owner-results-meta-label">Mailing:</span> ${escapeHtml(ownerAddressText)}</div>
                            </div>
                            <div class="owner-results-line3">Class: ${escapeHtml(classText)}</div>
                            <div class="owner-results-line3">Tax District: ${escapeHtml(taxDist)}</div>
                            <div class="owner-results-line3">Acres: ${escapeHtml(acresText)}</div>
                        </div>
                        <div class="owner-results-preview" data-feature-id="${escapeHtml(item.feature_id || "")}">
                            <div class="owner-results-preview-placeholder"></div>
                        </div>
                    </div>
                </button>
            `;
            })
            .join("");

        setupOwnerPreviewLazyLoading();

        const pageButtons = buildOwnerResultsPageButtons(ownerResultsPage, ownerResultsTotalPages);
        const prevDisabled = ownerResultsPage <= 1;
        const nextDisabled = ownerResultsPage >= ownerResultsTotalPages;
        ownerResultsPagination.innerHTML = `
            <button class="owner-results-page-btn" data-page-nav="prev" ${prevDisabled ? "disabled" : ""}>Prev</button>
            ${pageButtons
                .map(
                    (pageNumber) => `
                <button class="owner-results-page-btn ${pageNumber === ownerResultsPage ? "active" : ""}" data-page="${pageNumber}">
                    ${pageNumber}
                </button>
            `,
                )
                .join("")}
            <button class="owner-results-page-btn" data-page-nav="next" ${nextDisabled ? "disabled" : ""}>Next</button>
        `;
    }

    function clearOwnerPreviewTargets() {
        ownerPreviewTargets.clear();
        if (ownerPreviewObserver) {
            ownerPreviewObserver.disconnect();
        }
    }

    function setupOwnerPreviewLazyLoading() {
        clearOwnerPreviewTargets();

        ownerPreviewObserver = new IntersectionObserver(
            (entries) => {
                const toQueue = [];
                entries.forEach((entry) => {
                    if (!entry.isIntersecting) return;
                    const el = entry.target;
                    const featureID = el.dataset.featureId;
                    if (!featureID) return;
                    registerOwnerPreviewTarget(featureID, el);
                    if (ownerPreviewCache.has(featureID)) {
                        renderOwnerPreviewElement(el, ownerPreviewCache.get(featureID));
                        return;
                    }
                    toQueue.push(featureID);
                });
                if (toQueue.length) {
                    queueOwnerPreviewBatch(toQueue);
                }
            },
            { root: ownerResultsBody, rootMargin: "150px 0px 150px 0px" },
        );

        ownerResultsBody.querySelectorAll(".owner-results-preview[data-feature-id]").forEach((el) => {
            const featureID = el.dataset.featureId;
            if (featureID) {
                registerOwnerPreviewTarget(featureID, el);
                if (ownerPreviewCache.has(featureID)) {
                    renderOwnerPreviewElement(el, ownerPreviewCache.get(featureID));
                    return;
                }
            }
            ownerPreviewObserver.observe(el);
        });
    }

    function registerOwnerPreviewTarget(featureID, el) {
        let set = ownerPreviewTargets.get(featureID);
        if (!set) {
            set = new Set();
            ownerPreviewTargets.set(featureID, set);
        }
        set.add(el);
    }

    function queueOwnerPreviewBatch(featureIDs) {
        featureIDs.forEach((id) => {
            if (!id || ownerPreviewCache.has(id) || ownerPreviewInFlight.has(id)) return;
            ownerPreviewPendingIDs.add(id);
        });
        if (ownerPreviewBatchTimer) return;
        ownerPreviewBatchTimer = setTimeout(() => {
            ownerPreviewBatchTimer = null;
            flushOwnerPreviewBatch();
        }, 40);
    }

    async function flushOwnerPreviewBatch() {
        const ids = Array.from(ownerPreviewPendingIDs).slice(0, 25);
        if (!ids.length) return;
        ids.forEach((id) => {
            ownerPreviewPendingIDs.delete(id);
            ownerPreviewInFlight.add(id);
        });

        try {
            const payload = await fetchParcelPreviews(ids);
            const rows = Array.isArray(payload?.rows) ? payload.rows : [];
            const received = new Set();

            rows.forEach((row) => {
                const featureID = String(row?.feature_id || "");
                const geometry = row?.geometry;
                if (!featureID || !geometry) return;
                const preview = geometryToPreviewSvg(geometry);
                if (!preview) return;
                ownerPreviewCache.set(featureID, preview);
                received.add(featureID);
            });

            ids.forEach((id) => {
                const preview = ownerPreviewCache.get(id) || null;
                const targets = ownerPreviewTargets.get(id);
                if (!targets) return;
                targets.forEach((el) => {
                    if (preview) {
                        renderOwnerPreviewElement(el, preview);
                    } else {
                        el.innerHTML = '<div class="owner-results-preview-empty"></div>';
                    }
                });
            });
        } catch (_) {
            // Keep placeholders if preview fetch fails.
        } finally {
            ids.forEach((id) => ownerPreviewInFlight.delete(id));
            if (ownerPreviewPendingIDs.size > 0) {
                flushOwnerPreviewBatch();
            }
        }
    }

    function renderOwnerPreviewElement(el, preview) {
        if (!el || !preview) return;
        el.innerHTML = `
            <svg class="owner-results-preview-svg" viewBox="0 0 64 64" preserveAspectRatio="xMidYMid meet" aria-hidden="true">
                <path d="${preview.path}" class="owner-results-preview-path"></path>
            </svg>
        `;
    }

    function geometryToPreviewSvg(geometryJSON) {
        let geometry;
        try {
            geometry = typeof geometryJSON === "string" ? JSON.parse(geometryJSON) : geometryJSON;
        } catch (_) {
            return null;
        }
        if (!geometry || !geometry.type || !geometry.coordinates) return null;

        const rings = [];
        if (geometry.type === "Polygon") {
            geometry.coordinates.forEach((ring) => rings.push(ring));
        } else if (geometry.type === "MultiPolygon") {
            geometry.coordinates.forEach((poly) => poly.forEach((ring) => rings.push(ring)));
        } else {
            return null;
        }
        if (!rings.length) return null;

        let minX = Infinity;
        let minY = Infinity;
        let maxX = -Infinity;
        let maxY = -Infinity;
        rings.forEach((ring) => {
            ring.forEach((pt) => {
                const x = Number(pt?.[0]);
                const y = Number(pt?.[1]);
                if (!Number.isFinite(x) || !Number.isFinite(y)) return;
                if (x < minX) minX = x;
                if (y < minY) minY = y;
                if (x > maxX) maxX = x;
                if (y > maxY) maxY = y;
            });
        });
        if (!Number.isFinite(minX) || !Number.isFinite(minY) || !Number.isFinite(maxX) || !Number.isFinite(maxY)) {
            return null;
        }

        const w = Math.max(maxX - minX, 1e-9);
        const h = Math.max(maxY - minY, 1e-9);
        const s = Math.max(w, h);
        const pad = 5;
        const size = 64 - pad * 2;
        const offsetX = pad + ((s - w) / s) * (size / 2);
        const offsetY = pad + ((s - h) / s) * (size / 2);

        const toX = (x) => offsetX + ((x - minX) / s) * size;
        const toY = (y) => 64 - (offsetY + ((y - minY) / s) * size);

        const paths = [];
        rings.forEach((ring) => {
            if (!Array.isArray(ring) || ring.length < 3) return;
            const commands = ring
                .map((pt, idx) => {
                    const x = toX(Number(pt[0])).toFixed(2);
                    const y = toY(Number(pt[1])).toFixed(2);
                    return `${idx === 0 ? "M" : "L"}${x} ${y}`;
                })
                .join(" ");
            paths.push(`${commands} Z`);
        });
        if (!paths.length) return null;
        return { path: paths.join(" ") };
    }

    async function loadOwnerProperties({ ownerName = "", featureId = "", ownerAddress = "" } = {}, page = 1) {
        const trimmedOwner = (ownerName || "").trim();
        const trimmedFeatureID = (featureId || "").trim();
        const trimmedOwnerAddress = (ownerAddress || "").trim();
        if (!trimmedOwner && !trimmedFeatureID) return;

        const cacheKey = `${trimmedFeatureID.toLowerCase()}|${trimmedOwner.toLowerCase()}|${trimmedOwnerAddress.toLowerCase()}`;
        const cached = ownerResultsCache.get(cacheKey);
        if (cached) {
            ownerResultsOwnerName = cached.ownerName;
            ownerResultsOwnerAddress = cached.ownerAddress || "";
            ownerResultsFeatureID = cached.featureId || "";
            ownerResultsMatchType = cached.matchType || "";
            ownerResultsTotalProperties = cached.totalProperties;
            ownerResultsCountyCount = cached.countyCount;
            ownerResultsAllRows = cached.rows;
            ownerResultsTotalPages = Math.max(1, Math.ceil(ownerResultsAllRows.length / OWNER_RESULTS_PAGE_SIZE));
            ownerResultsPage = Math.min(Math.max(1, page), ownerResultsTotalPages);
            ownerResultsLoading = false;
            ownerResultsExpandedFeatureIDs = new Set();
            renderOwnerResultsPane();
            return;
        }

        if (ownerResultsAbortController) {
            ownerResultsAbortController.abort();
        }
        ownerResultsAbortController = new AbortController();

        ownerResultsOwnerName = trimmedOwner || "Owner Results";
        ownerResultsOwnerAddress = trimmedOwnerAddress;
        ownerResultsFeatureID = trimmedFeatureID;
        ownerResultsMatchType = "";
        ownerResultsLoading = true;
        ownerResultsPage = page;
        if (page === 1) {
            ownerResultsSelectedFeatureID = null;
            ownerResultsExpandedFeatureIDs = new Set();
        }
        renderOwnerResultsPane();

        try {
            const params = new URLSearchParams();
            if (trimmedFeatureID) params.set("feature_id", trimmedFeatureID);
            if (trimmedOwner) params.set("owner_name", trimmedOwner);
            if (trimmedOwnerAddress) params.set("owner_address", trimmedOwnerAddress);
            const payload = await fetchOwnerProperties(params, ownerResultsAbortController.signal);
            ownerResultsOwnerName = payload?.owner_name || trimmedOwner || "Owner Results";
            ownerResultsOwnerAddress = payload?.owner_address || trimmedOwnerAddress;
            ownerResultsFeatureID = payload?.feature_id || trimmedFeatureID;
            ownerResultsMatchType = payload?.match_type || "";
            ownerResultsTotalProperties = Number(payload?.total_properties || 0);
            ownerResultsCountyCount = Number(payload?.county_count || 0);
            ownerResultsAllRows = Array.isArray(payload?.results) ? payload.results : [];
            ownerResultsTotalPages = Math.max(1, Math.ceil(ownerResultsAllRows.length / OWNER_RESULTS_PAGE_SIZE));
            ownerResultsPage = Math.min(Math.max(1, page), ownerResultsTotalPages);
            ownerResultsLoading = false;
            ownerResultsExpandedFeatureIDs = new Set();
            ownerResultsCache.set(cacheKey, {
                ownerName: ownerResultsOwnerName,
                ownerAddress: ownerResultsOwnerAddress,
                featureId: ownerResultsFeatureID,
                matchType: ownerResultsMatchType,
                totalProperties: ownerResultsTotalProperties,
                countyCount: ownerResultsCountyCount,
                rows: ownerResultsAllRows,
            });
            renderOwnerResultsPane();
        } catch (err) {
            if (err.name === "AbortError") return;
            ownerResultsLoading = false;
            ownerResultsRows = [];
            ownerResultsAllRows = [];
            ownerResultsTotalPages = 0;
            ownerResultsBody.innerHTML = '<div class="owner-results-empty">Owner results are unavailable right now.</div>';
            ownerResultsPagination.innerHTML = "";
        }
    }

    function openOwnerResultsForOwnerContext({ ownerName = "", featureId = "", ownerAddress = "" } = {}) {
        const trimmedOwner = (ownerName || "").trim();
        const trimmedFeatureID = (featureId || "").trim();
        const trimmedOwnerAddress = (ownerAddress || "").trim();
        if (!trimmedOwner && !trimmedFeatureID) return;
        openOwnerResultsPane();
        loadOwnerProperties(
            {
                ownerName: trimmedOwner,
                featureId: trimmedFeatureID,
                ownerAddress: trimmedOwnerAddress,
            },
            1,
        );
    }

    function flyToOwnerResultsItem(item) {
        if (!item || !item.feature_id || !Number.isFinite(item.lat) || !Number.isFinite(item.lng)) return;
        ownerResultsSelectedFeatureID = item.feature_id;
        renderOwnerResultsPane();
        clearActivePopup();
        clearMapSelection();
        stopSearchFlyTrackingConflicts();
        setSelectedFeatureId(item.feature_id);

        map.flyTo({
            center: [item.lng, item.lat],
            zoom: Math.max(map.getZoom(), 18),
            speed: 1.2,
            curve: 1.4,
        });
        map.once("moveend", () => {
            map.setFeatureState(
                { source: "parcels", sourceLayer: "parcels", id: item.feature_id },
                { selected: true },
            );
            openParcelPopupFromSearch(item);
        });

        if (isMobile()) {
            closeOwnerResultsPane();
        }
    }

    ownerResultsBody.addEventListener("click", (e) => {
        const metaToggle = e.target.closest('[data-owner-meta-toggle="true"]');
        if (metaToggle) {
            e.preventDefault();
            e.stopPropagation();
            const featureID = String(metaToggle.getAttribute("data-feature-id") || "").trim();
            if (!featureID) return;
            if (ownerResultsExpandedFeatureIDs.has(featureID)) {
                ownerResultsExpandedFeatureIDs.delete(featureID);
            } else {
                ownerResultsExpandedFeatureIDs.add(featureID);
            }
            renderOwnerResultsPane();
            return;
        }
        if (e.target.closest(".owner-results-meta-panel")) {
            e.preventDefault();
            e.stopPropagation();
            return;
        }
        const row = e.target.closest(".owner-results-item");
        if (!row) return;
        const idx = Number(row.dataset.idx);
        if (Number.isNaN(idx) || idx < 0 || idx >= ownerResultsRows.length) return;
        flyToOwnerResultsItem(ownerResultsRows[idx]);
    });

    ownerResultsBody.addEventListener("keydown", (e) => {
        const metaToggle = e.target.closest('[data-owner-meta-toggle="true"]');
        if (!metaToggle) return;
        if (e.key !== "Enter" && e.key !== " ") return;
        e.preventDefault();
        metaToggle.click();
    });

    ownerResultsPagination.addEventListener("click", (e) => {
        const pageBtn = e.target.closest("[data-page]");
        if (pageBtn) {
            const page = Number(pageBtn.getAttribute("data-page"));
            if (Number.isFinite(page) && page > 0 && page !== ownerResultsPage) {
                ownerResultsPage = page;
                renderOwnerResultsPane();
            }
            return;
        }
        const navBtn = e.target.closest("[data-page-nav]");
        if (!navBtn) return;
        const dir = navBtn.getAttribute("data-page-nav");
        if (dir === "prev" && ownerResultsPage > 1) {
            ownerResultsPage -= 1;
            renderOwnerResultsPane();
        } else if (dir === "next" && ownerResultsPage < ownerResultsTotalPages) {
            ownerResultsPage += 1;
            renderOwnerResultsPane();
        }
    });

    return {
        openOwnerResultsForOwnerContext,
        openOwnerResultsPane,
        closeOwnerResultsPane,
    };
}

