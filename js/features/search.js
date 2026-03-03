// Handles search UI rendering, API calls, and result navigation.
import { searchParcels } from "../api.js";
import { escapeHtml } from "../utils/format.js";

export function initSearchFeature({
    map,
    closeOwnerResultsPane,
    clearActivePopup,
    clearMapSelection,
    stopSearchFlyTrackingConflicts,
    openParcelPopupFromSearch,
    setSelectedFeatureId,
}) {
    const searchContainer = document.createElement("div");
    searchContainer.className = "map-search";
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

    const searchInput = searchContainer.querySelector(".map-search-input");
    const searchClear = searchContainer.querySelector(".map-search-clear");
    const searchResults = searchContainer.querySelector(".map-search-results");
    const searchModeButtons = searchContainer.querySelectorAll(".map-search-mode");

    let searchItems = [];
    let searchActiveIndex = -1;
    let searchDebounceTimer = null;
    let searchAbortController = null;
    let lastSearchQuery = "";
    let searchMode = "address";
    let searchLoading = false;

    function closeSearchResults() {
        searchLoading = false;
        searchResults.style.display = "none";
        searchResults.innerHTML = "";
        searchItems = [];
        searchActiveIndex = -1;
    }

    function openSearchResults() {
        searchResults.style.display = "block";
    }

    function renderSearchLoading() {
        searchResults.innerHTML = `
            <div class="map-search-loading">
                <span class="map-search-spinner"></span>
                <span>Searching...</span>
            </div>
        `;
        openSearchResults();
    }

    function renderSearchResults() {
        if (searchLoading) {
            renderSearchLoading();
            return;
        }
        if (!searchItems.length) {
            searchResults.innerHTML = '<div class="map-search-empty">No matching Georgia parcels found.</div>';
            openSearchResults();
            return;
        }

        searchResults.innerHTML = searchItems
            .map(
                (item, idx) => `
            <button class="map-search-item ${idx === searchActiveIndex ? "active" : ""}" data-idx="${idx}">
                <div class="map-search-line1">${
                    searchMode === "owner"
                        ? escapeHtml(item.owner_name || "Unknown Owner")
                        : escapeHtml(item.display_address || item.site_address || "Unknown Address")
                }</div>
                <div class="map-search-line2">${
                    searchMode === "owner"
                        ? `${escapeHtml(item.site_address || "Unknown Address")} - ${escapeHtml(item.county_name || "Unknown County")} County, Georgia`
                        : item.mailing_city || item.mailing_zip5
                          ? `${escapeHtml(item.mailing_city || "Unknown City")}${item.mailing_zip5 ? `, GA ${escapeHtml(item.mailing_zip5)}` : ", GA"} - ${escapeHtml(item.county_name || "Unknown County")} County`
                          : `${escapeHtml(item.county_name || "Unknown County")} County, Georgia`
                }</div>
            </button>
        `,
            )
            .join("");
        openSearchResults();
    }

    function flyToSearchResult(item) {
        if (!item) return;
        clearActivePopup();
        clearMapSelection();
        stopSearchFlyTrackingConflicts();

        if (item.feature_id) {
            setSelectedFeatureId(item.feature_id);
        }

        map.flyTo({
            center: [item.lng, item.lat],
            zoom: Math.max(map.getZoom(), 18),
            speed: 1.2,
            curve: 1.4,
        });
        if (item.feature_id) {
            map.once("moveend", () => {
                map.setFeatureState(
                    { source: "parcels", sourceLayer: "parcels", id: item.feature_id },
                    { selected: true },
                );
                openParcelPopupFromSearch(item);
            });
        }
        searchInput.value = item.display_address || item.site_address || "";
        closeSearchResults();
    }

    async function runAddressSearch(query) {
        const trimmed = query.trim();
        if (trimmed.length < 2) {
            searchLoading = false;
            lastSearchQuery = "";
            closeSearchResults();
            return;
        }
        const normalized = trimmed.toLowerCase().replace(/\s+/g, " ");
        const cacheKey = `${searchMode}|${normalized}`;
        if (cacheKey === lastSearchQuery) {
            return;
        }
        lastSearchQuery = cacheKey;

        if (searchAbortController) {
            searchAbortController.abort();
        }
        searchAbortController = new AbortController();
        searchLoading = true;
        renderSearchLoading();

        try {
            const payload = await searchParcels(trimmed, searchMode, 10, searchAbortController.signal);
            searchLoading = false;
            searchItems = Array.isArray(payload.results) ? payload.results : [];
            searchActiveIndex = searchItems.length ? 0 : -1;
            renderSearchResults();
        } catch (err) {
            if (err.name === "AbortError") return;
            searchLoading = false;
            searchItems = [];
            searchActiveIndex = -1;
            searchResults.innerHTML = '<div class="map-search-empty">Search unavailable right now.</div>';
            openSearchResults();
        }
    }

    searchInput.addEventListener("input", () => {
        if (searchMode === "address") {
            closeOwnerResultsPane();
        }
        if (searchDebounceTimer) {
            clearTimeout(searchDebounceTimer);
        }
        searchDebounceTimer = setTimeout(() => {
            runAddressSearch(searchInput.value);
        }, 280);
    });

    searchModeButtons.forEach((btn) => {
        btn.addEventListener("click", () => {
            const nextMode = btn.dataset.mode;
            if (nextMode === searchMode) return;
            searchMode = nextMode;
            searchInput.placeholder = searchMode === "owner" ? "Search Georgia owner..." : "Search Georgia address...";
            lastSearchQuery = "";
            searchModeButtons.forEach((el) => {
                el.classList.toggle("active", el.dataset.mode === searchMode);
            });
            runAddressSearch(searchInput.value);
        });
    });

    searchInput.addEventListener("keydown", (e) => {
        if (searchMode === "address") {
            closeOwnerResultsPane();
        }
        if (!searchItems.length) {
            if (e.key === "Enter") {
                e.preventDefault();
                runAddressSearch(searchInput.value);
            }
            if (e.key === "Escape") {
                closeSearchResults();
            }
            return;
        }

        if (e.key === "ArrowDown") {
            e.preventDefault();
            searchActiveIndex = (searchActiveIndex + 1) % searchItems.length;
            renderSearchResults();
        } else if (e.key === "ArrowUp") {
            e.preventDefault();
            searchActiveIndex = (searchActiveIndex - 1 + searchItems.length) % searchItems.length;
            renderSearchResults();
        } else if (e.key === "Enter") {
            e.preventDefault();
            const idx = searchActiveIndex >= 0 ? searchActiveIndex : 0;
            flyToSearchResult(searchItems[idx]);
        } else if (e.key === "Escape") {
            e.preventDefault();
            closeSearchResults();
        }
    });

    searchResults.addEventListener("click", (e) => {
        const button = e.target.closest(".map-search-item");
        if (!button) return;
        const idx = Number(button.dataset.idx);
        if (Number.isNaN(idx) || idx < 0 || idx >= searchItems.length) return;
        flyToSearchResult(searchItems[idx]);
    });

    searchClear.addEventListener("click", () => {
        searchInput.value = "";
        lastSearchQuery = "";
        closeSearchResults();
        searchInput.focus();
    });

    document.addEventListener("click", (e) => {
        if (!searchContainer.contains(e.target)) {
            closeSearchResults();
        }
    });
}

