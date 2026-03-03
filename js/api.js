async function fetchJSON(url, options = {}, errorPrefix = "Request failed") {
    const response = await fetch(url, options);
    if (!response.ok) {
        throw new Error(`${errorPrefix} (${response.status})`);
    }
    return response.json();
}

export function fetchStyle(styleId) {
    return fetchJSON(`/styles/${styleId}.json`, {}, "Style fetch failed");
}

export function searchParcels(query, mode, limit, signal) {
    const q = encodeURIComponent(query);
    const m = encodeURIComponent(mode);
    return fetchJSON(
        `/api/search/parcels?q=${q}&limit=${limit}&mode=${m}`,
        { method: "GET", signal },
        "Search failed",
    );
}

export function fetchOwnerProperties(params, signal) {
    return fetchJSON(
        `/api/owners/properties?${params.toString()}`,
        { signal },
        "Owner properties query failed",
    );
}

export function fetchParcelDetailsByFeatureId(featureId) {
    return fetchJSON(
        `/api/parcels/${encodeURIComponent(featureId)}`,
        {},
        "Parcel lookup failed",
    );
}

export function fetchParcelTaxHistoryByFeatureId(featureId) {
    return fetchJSON(
        `/api/parcels/${encodeURIComponent(featureId)}/taxes`,
        {},
        "Parcel tax history lookup failed",
    );
}

export function fetchCountyCentroidById(countyId) {
    return fetchJSON(
        `/api/counties/${encodeURIComponent(String(countyId))}/centroid`,
        {},
        "County centroid lookup failed",
    );
}

export function fetchTaxHeatmapParcelValues(year, countyId, objectIds) {
    return fetchJSON(
        "/api/tax-heatmap/parcel-values",
        {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                year,
                county_id: countyId,
                object_ids: objectIds,
            }),
        },
        "Tax parcel values failed",
    );
}

export function fetchTaxHeatmapStatsByYear(countyId, year) {
    return fetchJSON(
        `/api/tax-heatmap/stats?county_id=${countyId}&year=${year}`,
        {},
        "Tax heatmap stats failed",
    );
}

export function fetchParcelPreviews(featureIds) {
    return fetchJSON(
        "/api/parcels/previews",
        {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ feature_ids: featureIds }),
        },
        "Parcel previews failed",
    );
}
