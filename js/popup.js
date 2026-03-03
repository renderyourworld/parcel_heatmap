import {
    escapeHtml,
    formatLocalDateTime,
    normalizeAddressText,
    toNumberOrNull,
} from "./utils/format.js";

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

export function createPopupHTML({
    currentStyle,
    title,
    accent,
    rows,
    maxWidth = 320,
    footerText = '',
    footerHtml = '',
    titleAlign = 'left',
    titleClass = '',
    mobileScale = 1,
    isMobile = false
}) {
    const t = getPopupTheme(currentStyle);
    const mobile = typeof isMobile === 'function' ? Boolean(isMobile()) : Boolean(isMobile);
    const labelColWidth = mobile ? 78 : 96;
    const rowPadY = mobile ? 4 : 5;
    const titleSize = mobile ? 14 : 15;
    const bodySize = mobile ? 12 : 13;
    const cardPadTop = mobile ? 10 : 12;
    const cardPadSide = mobile ? 10 : 12;
    const cardPadBottom = mobile ? 7 : 8;
    const normalizedMobileScale = Number.isFinite(mobileScale)
        ? Math.min(Math.max(mobileScale, 0.7), 1)
        : 1;
    const mobileCap = Math.min(maxWidth, 320) * normalizedMobileScale;
    const effectiveMaxWidth = mobile
        ? `min(${mobileCap}px, calc(100vw - 24px))`
        : `${maxWidth}px`;

    const rowHTML = rows.map((r) => {
        const valueContent = r.valueHtml !== undefined ? r.valueHtml : escapeHtml(r.value ?? '');
        if (r.inline) {
            return `
                <div style="padding:${rowPadY}px 0; border-bottom:1px solid ${t.border};">
                    <span style="font-weight:600; color:${t.text};">${escapeHtml(r.label)}:</span>
                    <span style="color:${t.muted}; margin-left:6px;">${valueContent}</span>
                </div>
            `;
        }
        if (r.stacked) {
            return `
                <div style="padding:${rowPadY}px 0; border-bottom:1px solid ${t.border};">
                    <div style="font-weight:600; color:${t.text}; margin-bottom:2px;">${escapeHtml(r.label)}:</div>
                    <div style="color:${t.muted};">${valueContent}</div>
                </div>
            `;
        }
        return `
            <div style="display:grid; grid-template-columns: ${labelColWidth}px 1fr; column-gap:10px; padding:${rowPadY}px 0; border-bottom:1px solid ${t.border};">
                <div style="font-weight:600; color:${t.text};">${escapeHtml(r.label)}</div>
                <div style="color:${t.muted};">${valueContent}</div>
            </div>
        `;
    }).join('');
    let footerBlock = '';
    if (footerHtml) {
        footerBlock = `<div style="margin-top:8px; font-size:${mobile ? 11 : 11.5}px; color:${t.muted}; opacity:0.95;">${footerHtml}</div>`;
    } else if (footerText) {
        footerBlock = `<div style="margin-top:8px; font-size:${mobile ? 11 : 11.5}px; color:${t.muted}; opacity:0.9;">${footerText}</div>`;
    }

    const titleHTML = title
        ? `
            <div style="
                margin:0 0 10px 0;
                font-size:${titleSize}px;
                font-weight:700;
                letter-spacing:0.01em;
                padding-bottom:8px;
                border-bottom:2px solid ${accent};
                color:${t.text};
                text-align:${titleAlign};
            " class="${titleClass}">${title}</div>
        `
        : '';

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
            ${titleHTML}
            <div>${rowHTML}</div>
            ${footerBlock}
        </div>
    `;
}

export function buildSiteAddressCopyText(details, tileProps) {
    const displayAddress = String(details.display_address || '').trim();
    if (displayAddress) return displayAddress;
    return String(details.site_address || tileProps.site_address || '').trim();
}

export function buildOwnerAddressCopyText(details) {
    return String(details.owner_address || '').trim();
}

export function buildAddressValueHtml({ value, copyAction, copyText, currentStyle }) {
    const safeValue = escapeHtml(value || 'N/A');
    const normalizedCopyText = normalizeAddressText(copyText);
    const darkTheme = currentStyle === 'dark' || currentStyle === 'black';
    const btnBorder = darkTheme ? 'rgba(172, 190, 208, 0.45)' : 'rgba(82,97,112,0.35)';
    const btnBg = darkTheme ? 'rgba(172, 190, 208, 0.12)' : 'rgba(82,97,112,0.09)';
    const btnText = darkTheme ? '#d7e5f2' : '#4f6071';

    if (!normalizedCopyText) {
        return `<span>${safeValue}</span>`;
    }

    return `
        <div style="display:flex; align-items:flex-start; justify-content:space-between; gap:8px;">
            <span style="min-width:0;">${safeValue}</span>
            <button
                type="button"
                data-popup-action="${copyAction}"
                data-copy-text="${escapeHtml(normalizedCopyText)}"
                title="Copy address"
                aria-label="Copy address"
                style="
                    border:1px solid ${btnBorder};
                    background:${btnBg};
                    color:${btnText};
                    border-radius:6px;
                    width:22px;
                    height:22px;
                    display:inline-flex;
                    align-items:center;
                    justify-content:center;
                    cursor:pointer;
                    flex:0 0 auto;
                    padding:0;
                    line-height:1;
                "
            >
                <svg width="12" height="12" viewBox="0 0 24 24" aria-hidden="true" style="display:block;">
                    <path fill="currentColor" d="M8 7a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2h-8a2 2 0 0 1-2-2V7zm2 0v11h8V7h-8zM5 4h9v2H5v11h2v2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2z"/>
                </svg>
            </button>
        </div>
    `;
}

export function flashCopyButton(btn, copied) {
    if (!btn) return;
    const original = btn.innerHTML;
    btn.disabled = true;
    btn.style.opacity = '0.9';
    btn.innerHTML = copied
        ? '<span style="font-size:12px; line-height:1;">&#10003;</span>'
        : '<span style="font-size:12px; line-height:1;">!</span>';
    setTimeout(() => {
        btn.innerHTML = original;
        btn.disabled = false;
        btn.style.opacity = '';
    }, 900);
}

export function buildParcelFooter({ currentStyle, lat, lng, updatedAt }) {
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




// Manages county selection popup behavior and viewport-safe positioning.
export function initCountyPopupFeature({
    map,
    maplibregl,
    isMobile,
    getCurrentStyle,
    createPopupHTML,
    formatNumber,
    setActivePopup,
    clearCountySelection,
    setSelectedCountyId,
    fetchCountyCentroid,
}) {
    let activeCountyPopupState = null;

    function hasActiveCountyPopup() {
        return Boolean(activeCountyPopupState && activeCountyPopupState.popup && activeCountyPopupState.popup.isOpen());
    }

    function getCountyPopupEdgePadding() {
        if (isMobile()) {
            return { left: 12, right: 12, top: 116, bottom: 20 };
        }
        return { left: 20, right: 96, top: 84, bottom: 20 };
    }

    function alignCountyPopupTitleToAnchor(popup, lngLat) {
        if (!popup || !popup.isOpen() || !lngLat) return;
        const popupEl = popup.getElement();
        if (!popupEl) return;
        const titleEl = popupEl.querySelector(".county-popup-title");
        if (!titleEl) return;

        const anchorPt = map.project(lngLat);
        const rect = titleEl.getBoundingClientRect();
        const titleCenterX = rect.left + rect.width / 2;
        const titleCenterY = rect.top + rect.height / 2;
        const dx = Math.round(anchorPt.x - titleCenterX);
        const dy = Math.round(anchorPt.y - titleCenterY);
        popup.setOffset([dx, dy]);
    }

    function syncCountyPopupVisibility() {
        if (!activeCountyPopupState || !activeCountyPopupState.popup || !activeCountyPopupState.popup.isOpen()) {
            activeCountyPopupState = null;
            return;
        }

        const anchorLngLat = activeCountyPopupState.anchorLngLat || activeCountyPopupState.lngLat;
        const pt = map.project(anchorLngLat);
        const container = map.getContainer();
        const w = container.clientWidth;
        const h = container.clientHeight;
        const pad = getCountyPopupEdgePadding();

        const farOffscreen = pt.x < -w || pt.x > w * 2 || pt.y < -h || pt.y > h * 2;
        if (farOffscreen) {
            activeCountyPopupState.popup.remove();
            activeCountyPopupState = null;
            clearCountySelection();
            return;
        }

        const inPaddedView = pt.x >= pad.left && pt.x <= w - pad.right && pt.y >= pad.top && pt.y <= h - pad.bottom;
        if (inPaddedView) {
            activeCountyPopupState.popup.setLngLat(anchorLngLat);
            return;
        }

        const clampedPoint = {
            x: Math.min(Math.max(pt.x, pad.left), Math.max(pad.left, w - pad.right)),
            y: Math.min(Math.max(pt.y, pad.top), Math.max(pad.top, h - pad.bottom)),
        };
        const clampedLngLat = map.unproject([clampedPoint.x, clampedPoint.y]);
        activeCountyPopupState.popup.setLngLat(clampedLngLat);
    }

    async function handleCountyClick(e) {
        if (e.features.length === 0) return;
        if (map.getZoom() >= 13) return;

        const feature = e.features[0];
        clearCountySelection();

        const nextCountyId = feature.id;
        setSelectedCountyId(nextCountyId);
        map.setFeatureState({ source: "counties", sourceLayer: "counties", id: nextCountyId }, { selected: true });

        const props = feature.properties;
        const popupHTML = createPopupHTML({
            currentStyle: getCurrentStyle(),
            title: `${props.name} County`,
            accent: "#5f88a6",
            titleAlign: "center",
            titleClass: "county-popup-title",
            maxWidth: 280,
            isMobile,
            rows: [
                { label: "Population", value: formatNumber(props.population) },
                { label: "Region", value: props.region || "N/A" },
                { label: "Acres", value: formatNumber(props.acres) },
                { label: "Sq. Miles", value: formatNumber(props.square_miles) },
            ],
        });

        let anchorLngLat = e.lngLat;
        try {
            const centroid = await fetchCountyCentroid(feature.id);
            if (centroid && Number.isFinite(centroid.lat) && Number.isFinite(centroid.lng)) {
                anchorLngLat = { lng: centroid.lng, lat: centroid.lat };
            }
        } catch (_) {
            anchorLngLat = e.lngLat;
        }

        const popup = new maplibregl.Popup({ offset: [0, 0] }).setLngLat(anchorLngLat).setHTML(popupHTML).addTo(map);
        setActivePopup(popup);
        alignCountyPopupTitleToAnchor(popup, anchorLngLat);

        activeCountyPopupState = {
            countyID: nextCountyId,
            popup,
            lngLat: anchorLngLat,
            anchorLngLat,
        };

        popup.on("close", () => {
            if (activeCountyPopupState && activeCountyPopupState.popup === popup) {
                activeCountyPopupState = null;
            }
        });

        syncCountyPopupVisibility();
    }

    function bindEvents() {
        map.on("click", "county-fill", handleCountyClick);
        map.on("mouseenter", "county-fill", () => {
            map.getCanvas().style.cursor = "pointer";
        });
        map.on("mouseleave", "county-fill", () => {
            map.getCanvas().style.cursor = "";
        });
    }

    bindEvents();

    return {
        hasActiveCountyPopup,
        syncCountyPopupVisibility,
    };
}




// Builds parcel popup views and wires popup-driven actions.
export function initParcelPopupFeature({
    map,
    maplibregl,
    isMobile,
    getCurrentStyle,
    createParcelPopupHTML,
    recenterParcelPopup,
    setActivePopup,
    getSelectedFeatureId,
    setSelectedFeatureId,
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
    getOpenOwnerResultsForOwnerContext,
}) {
    function buildParcelLoadingPopup(props) {
        return createParcelPopupHTML({
            currentStyle: getCurrentStyle(),
            title: "",
            accent: "#2f6ea9",
            maxWidth: 360,
            rows: [
                { label: "Address", value: props.site_address || "N/A", stacked: true },
                { label: "Acres", value: formatNumber(props.acres), inline: true },
                { label: "Owner", valueHtml: '<span class="popup-skeleton-line popup-skeleton-long"></span>', stacked: true },
                { label: "Class", valueHtml: '<span class="popup-skeleton-line popup-skeleton-medium"></span>', inline: true },
                { label: "Tax District", valueHtml: '<span class="popup-skeleton-line popup-skeleton-short"></span>', inline: true },
            ],
            footerText: "Loading full parcel details...",
        });
    }

    function buildParcelDetailsFooterHtml({ currentStyle, lat, lng, updatedAt }) {
        const darkTheme = currentStyle === "dark" || currentStyle === "black";
        const btnBorder = darkTheme ? "rgba(126, 179, 230, 0.55)" : "rgba(47, 110, 169, 0.35)";
        const btnBg = darkTheme ? "rgba(79, 136, 191, 0.22)" : "rgba(47, 110, 169, 0.10)";
        const btnText = darkTheme ? "#d4e9ff" : "#2f6ea9";
        const ownerBtnBorder = darkTheme ? "rgba(172, 190, 208, 0.5)" : "rgba(82,97,112,0.35)";
        const ownerBtnBg = darkTheme ? "rgba(172, 190, 208, 0.16)" : "rgba(82,97,112,0.10)";
        const ownerBtnText = darkTheme ? "#d7e5f2" : "#2f3f4f";
        const mapsBtnBorder = darkTheme ? "rgba(172, 190, 208, 0.5)" : "rgba(82,97,112,0.35)";
        const mapsBtnBg = darkTheme ? "rgba(172, 190, 208, 0.16)" : "rgba(82,97,112,0.10)";
        const mapsBtnText = darkTheme ? "#d7e5f2" : "#2f3f4f";
        const parsedLat = toNumberOrNull(lat);
        const parsedLng = toNumberOrNull(lng);
        const hasCoords = parsedLat !== null && parsedLng !== null;
        const mapsHref = hasCoords ? `https://www.google.com/maps?q=${parsedLat.toFixed(6)},${parsedLng.toFixed(6)}` : "";
        const updatedLine = `Last Updated At: ${formatLocalDateTime(updatedAt)}`;
        const mapsButton = hasCoords
            ? `<a href="${mapsHref}" target="_blank" rel="noopener noreferrer" style="
                    border: 1px solid ${mapsBtnBorder};
                    background: ${mapsBtnBg};
                    color: ${mapsBtnText};
                    border-radius: 8px;
                    padding: 0 10px;
                    font-size: 12px;
                    cursor: pointer;
                    text-decoration: none;
                    display: inline-flex;
                    align-items: center;
                    justify-content: center;
                    gap: 6px;
                    min-height: 28px;
                    line-height: 1;
                    box-sizing: border-box;
                    flex: 1.25 1 0;
                    white-space: nowrap;
                ">
                    <svg width="13" height="13" viewBox="0 0 24 24" aria-hidden="true" style="display:block;">
                        <path fill="currentColor" d="M12 2a7 7 0 0 0-7 7c0 5.25 7 13 7 13s7-7.75 7-13a7 7 0 0 0-7-7zm0 9.25A2.25 2.25 0 1 1 12 6.75a2.25 2.25 0 0 1 0 4.5z"/>
                    </svg>
                    <span>Google Maps</span>
                </a>`
            : "";
        return `
            <div style="margin-top:8px; display:flex; flex-wrap:wrap; gap:8px;">
                ${mapsButton}
                <button data-popup-action="view-taxes" style="
                    border: 1px solid ${btnBorder};
                    background: ${btnBg};
                    color: ${btnText};
                    border-radius: 8px;
                    padding: 0 10px;
                    font-size: 12px;
                    cursor: pointer;
                    display: flex;
                    align-items: center;
                    min-height: 28px;
                    line-height: 1;
                    box-sizing: border-box;
                    flex: 1 1 0;
                    justify-content: center;
                    white-space: nowrap;
                ">View Taxes</button>
                <button data-popup-action="owner-properties" style="
                    border: 1px solid ${ownerBtnBorder};
                    background: ${ownerBtnBg};
                    color: ${ownerBtnText};
                    border-radius: 8px;
                    padding: 0 10px;
                    font-size: 12px;
                    cursor: pointer;
                    display:flex;
                    align-items:center;
                    justify-content:center;
                    gap:6px;
                    min-height: 28px;
                    line-height: 1;
                    box-sizing: border-box;
                    width:calc(100% - 4px);
                    flex-basis:100%;
                    margin:0 auto;
                ">
                    <svg width="12" height="12" viewBox="0 0 24 24" aria-hidden="true" style="display:block;">
                        <path fill="currentColor" d="M10 2a8 8 0 1 0 4.9 14.3l4.4 4.4 1.4-1.4-4.4-4.4A8 8 0 0 0 10 2zm0 2a6 6 0 1 1 0 12 6 6 0 0 1 0-12z"/>
                    </svg>
                    <span>Properties by Owner</span>
                </button>
            </div>
            <div style="margin-top:8px; padding-top:8px; border-top:1px solid rgba(127, 142, 160, 0.25); font-size:${isMobile() ? 11 : 11.5}px; color:${darkTheme ? "#b7c4d1" : "#526170"}; opacity:0.9;">
                ${updatedLine}
            </div>
        `;
    }

    function buildParcelTaxHistoryHtml(taxRows, currentStyle) {
        const darkTheme = currentStyle === "dark" || currentStyle === "black";
        const tableBorder = darkTheme ? "rgba(172, 190, 208, 0.28)" : "rgba(127,142,160,0.2)";
        const rowBorder = darkTheme ? "rgba(172, 190, 208, 0.2)" : "rgba(127,142,160,0.2)";
        const headerBg = darkTheme ? "rgba(30, 40, 52, 0.98)" : "rgba(255,255,255,0.96)";
        const headerText = darkTheme ? "#d8e7f6" : "#5f6f80";
        const bodyText = darkTheme ? "#d2dfec" : "#2f3f4f";
        if (!taxRows.length) {
            return `<div style="margin-top:8px; font-size:12px; color:${darkTheme ? "#b5c3d1" : "#6b7a88"};">No tax history available.</div>`;
        }

        const rowsHtml = taxRows
            .map((row) => {
                const year = row?.tax_year ?? "N/A";
                const taxAmount = formatCurrency(row?.tax_amount);
                const millage = row?.millage === null || row?.millage === undefined ? "N/A" : Number(row.millage).toFixed(3);
                return `
                <tr>
                    <td style="padding:6px 8px; border-bottom:1px solid ${rowBorder}; color:${bodyText};">${year}</td>
                    <td style="padding:6px 8px; border-bottom:1px solid ${rowBorder}; color:${bodyText};">${taxAmount}</td>
                    <td style="padding:6px 8px; border-bottom:1px solid ${rowBorder}; color:${bodyText};">${millage}</td>
                </tr>
            `;
            })
            .join("");

        const maxHeight = taxRows.length > 10 ? "220px" : "none";
        return `
            <div style="margin-top:8px; max-height:${maxHeight}; overflow-y:auto; border:1px solid ${tableBorder}; border-radius:8px;">
                <table style="width:100%; border-collapse:collapse; font-size:12px;">
                    <thead style="position:sticky; top:0; background:${headerBg};">
                        <tr>
                            <th style="text-align:left; padding:6px 8px; border-bottom:1px solid ${tableBorder}; color:${headerText};">Year</th>
                            <th style="text-align:left; padding:6px 8px; border-bottom:1px solid ${tableBorder}; color:${headerText};">Tax Amount</th>
                            <th style="text-align:left; padding:6px 8px; border-bottom:1px solid ${tableBorder}; color:${headerText};">Millage</th>
                        </tr>
                    </thead>
                    <tbody>${rowsHtml}</tbody>
                </table>
            </div>
        `;
    }

    function buildTaxTrendSparklineHtml(taxRows) {
        const points = taxRows
            .slice()
            .reverse()
            .map((row) => ({ year: row?.tax_year, amount: Number(row?.tax_amount) }))
            .filter((p) => Number.isFinite(p.amount));

        if (points.length < 2) {
            return '<div style="margin-top:8px; font-size:12px; color:#6b7a88;">Tax trend unavailable.</div>';
        }

        const width = 320;
        const height = 70;
        const padX = 10;
        const padY = 10;
        const min = Math.min(...points.map((p) => p.amount));
        const max = Math.max(...points.map((p) => p.amount));
        const span = max - min || 1;
        const stepX = (width - padX * 2) / (points.length - 1);

        const polyline = points
            .map((p, i) => {
                const x = padX + i * stepX;
                const y = padY + (height - padY * 2) * (1 - (p.amount - min) / span);
                return `${x.toFixed(1)},${y.toFixed(1)}`;
            })
            .join(" ");

        const lastYear = points[points.length - 1].year ?? "";
        const firstYear = points[0].year ?? "";

        return `
            <div style="margin-top:8px; border:1px solid rgba(127,142,160,0.2); border-radius:8px; padding:6px;">
                <div style="font-size:11px; color:#5f6f80; margin-bottom:4px;">Tax Amount Trend (${firstYear} - ${lastYear})</div>
                <svg width="100%" viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" style="display:block; height:74px;">
                    <polyline fill="none" stroke="#2f6ea9" stroke-width="2" points="${polyline}" />
                </svg>
            </div>
        `;
    }

    function buildParcelTaxesPopup(tileProps, details, taxRows) {
        const currentStyle = getCurrentStyle();
        const siteAddress = details.site_address || tileProps.site_address || "N/A";
        const historyHtml = buildParcelTaxHistoryHtml(taxRows, currentStyle);
        const sparklineHtml = buildTaxTrendSparklineHtml(taxRows);
        const darkTheme = currentStyle === "dark" || currentStyle === "black";
        const backBtnBorder = darkTheme ? "rgba(172, 190, 208, 0.5)" : "rgba(82,97,112,0.35)";
        const backBtnBg = darkTheme ? "rgba(172, 190, 208, 0.16)" : "rgba(82,97,112,0.10)";
        const backBtnText = darkTheme ? "#d7e5f2" : "#2f3f4f";
        return createParcelPopupHTML({
            currentStyle,
            title: `${siteAddress} - Taxes`,
            accent: "#2f6ea9",
            maxWidth: 400,
            rows: [],
            footerHtml: `
                ${sparklineHtml}
                ${historyHtml}
                <div style="margin-top:8px;">
                    <button data-popup-action="back-details" style="
                        border: 1px solid ${backBtnBorder};
                        background: ${backBtnBg};
                        color: ${backBtnText};
                        border-radius: 8px;
                        padding: 5px 10px;
                        font-size: 12px;
                        cursor: pointer;
                    ">Back</button>
                </div>
            `,
        });
    }

    function buildParcelDetailsPopup(tileProps, details) {
        const currentStyle = getCurrentStyle();
        const siteAddress = details.site_address || tileProps.site_address || "N/A";
        const ownerAddress = details.owner_address || "N/A";
        const acres = details.acres !== null && details.acres !== undefined ? details.acres : tileProps.acres;
        const classValue = `${details.category || ""} (${details.classification || "N/A"})`;
        const taxDistrict = details.tax_district || "N/A";
        const taxDistrictInline = taxDistrict.length <= 14;
        const lat = details.lat !== null && details.lat !== undefined ? details.lat : tileProps.lat;
        const lng = details.lng !== null && details.lng !== undefined ? details.lng : tileProps.lng;
        const siteAddressCopy = buildSiteAddressCopyText(details, tileProps);
        const ownerAddressCopy = buildOwnerAddressCopyText(details);

        return createParcelPopupHTML({
            currentStyle,
            title: "",
            accent: "#2f6ea9",
            maxWidth: 360,
            rows: [
                {
                    label: "Address",
                    valueHtml: buildAddressValueHtml({
                        value: siteAddress,
                        copyAction: "copy-site-address",
                        copyText: siteAddressCopy,
                        currentStyle,
                    }),
                    stacked: true,
                },
                { label: "Owner", value: details.owner_name || "N/A", stacked: true },
                {
                    label: "Owner Address",
                    valueHtml: buildAddressValueHtml({
                        value: ownerAddress,
                        copyAction: "copy-owner-address",
                        copyText: ownerAddressCopy,
                        currentStyle,
                    }),
                    stacked: true,
                },
                { label: "Acres", value: formatNumber(acres), inline: true },
                { label: "Class", value: classValue, inline: true },
                taxDistrictInline ? { label: "Tax District", value: taxDistrict, inline: true } : { label: "Tax District", value: taxDistrict, stacked: true },
            ],
            footerHtml: buildParcelDetailsFooterHtml({ currentStyle, lat, lng, updatedAt: details.updated_at }),
        });
    }

    function wireParcelPopupActions(popup, featureId, tileProps, details) {
        if (!popup || !popup.isOpen()) return;
        const popupEl = popup.getElement();
        if (!popupEl) return;

        const copyButtons = popupEl.querySelectorAll('[data-popup-action="copy-site-address"], [data-popup-action="copy-owner-address"]');
        copyButtons.forEach((btn) => {
            btn.addEventListener("click", async () => {
                const text = btn.getAttribute("data-copy-text") || "";
                const copied = await copyTextToClipboard(text);
                flashCopyButton(btn, copied);
            });
        });

        const viewTaxesBtn = popupEl.querySelector('[data-popup-action="view-taxes"]');
        if (viewTaxesBtn) {
            viewTaxesBtn.addEventListener("click", async () => {
                if (!popup.isOpen()) return;
                popup.setHTML(
                    createParcelPopupHTML({
                        currentStyle: getCurrentStyle(),
                        title: `${details.site_address || tileProps.site_address || "Parcel"} - Taxes`,
                        accent: "#2f6ea9",
                        maxWidth: 400,
                        rows: [{ label: "Status", value: "Loading tax history..." }],
                    }),
                );
                recenterParcelPopup(popup);

                try {
                    const taxRows = await fetchParcelTaxHistory(featureId);
                    if (getSelectedFeatureId() !== featureId || !popup.isOpen()) return;
                    popup.setHTML(buildParcelTaxesPopup(tileProps, details, taxRows));
                    wireParcelPopupActions(popup, featureId, tileProps, details);
                    recenterParcelPopup(popup);
                } catch (err) {
                    if (getSelectedFeatureId() !== featureId || !popup.isOpen()) return;
                    const currentStyle = getCurrentStyle();
                    const darkTheme = currentStyle === "dark" || currentStyle === "black";
                    const backBtnBorder = darkTheme ? "rgba(172, 190, 208, 0.5)" : "rgba(82,97,112,0.35)";
                    const backBtnBg = darkTheme ? "rgba(172, 190, 208, 0.16)" : "rgba(82,97,112,0.10)";
                    const backBtnText = darkTheme ? "#d7e5f2" : "#2f3f4f";
                    popup.setHTML(
                        createParcelPopupHTML({
                            currentStyle,
                            title: `${details.site_address || tileProps.site_address || "Parcel"} - Taxes`,
                            accent: "#2f6ea9",
                            maxWidth: 400,
                            rows: [{ label: "Error", value: "Unable to load tax history" }],
                            footerHtml: `<button data-popup-action="back-details" style="
                            border: 1px solid ${backBtnBorder};
                            background: ${backBtnBg};
                            color: ${backBtnText};
                            border-radius: 8px;
                            padding: 5px 10px;
                            font-size: 12px;
                            cursor: pointer;
                        ">Back</button>`,
                        }),
                    );
                    wireParcelPopupActions(popup, featureId, tileProps, details);
                    recenterParcelPopup(popup);
                }
            });
        }

        const backBtn = popupEl.querySelector('[data-popup-action="back-details"]');
        if (backBtn) {
            backBtn.addEventListener("click", () => {
                if (!popup.isOpen()) return;
                popup.setHTML(buildParcelDetailsPopup(tileProps, details));
                wireParcelPopupActions(popup, featureId, tileProps, details);
                recenterParcelPopup(popup);
            });
        }

        const ownerPropsBtn = popupEl.querySelector('[data-popup-action="owner-properties"]');
        if (ownerPropsBtn) {
            ownerPropsBtn.addEventListener("click", () => {
                const ownerName = String(details?.owner_name || tileProps?.owner_name || "").trim();
                const ownerAddress = String(details?.owner_address || "").trim();
                const contextFeatureID = String(featureId || details?.feature_id || "").trim();
                if (!ownerName && !contextFeatureID) return;
                const openOwnerResultsForOwnerContext = getOpenOwnerResultsForOwnerContext?.();
                if (typeof openOwnerResultsForOwnerContext !== "function") return;
                openOwnerResultsForOwnerContext({ ownerName, featureId: contextFeatureID, ownerAddress });
            });
        }
    }

    function renderDetailsIntoPopup(popup, featureId, tileProps, details) {
        popup.setHTML(buildParcelDetailsPopup(tileProps, details));
        wireParcelPopupActions(popup, featureId, tileProps, details);
        recenterParcelPopup(popup);
    }

    async function openParcelPopupFromSearch(item) {
        if (!item || !item.feature_id || item.lat === undefined || item.lng === undefined) {
            return;
        }

        const tileProps = {
            feature_id: item.feature_id,
            site_address: item.site_address || "",
            acres: null,
            lat: item.lat,
            lng: item.lng,
        };

        const popup = new maplibregl.Popup({ focusAfterOpen: false }).setLngLat([item.lng, item.lat]).setHTML(buildParcelLoadingPopup(tileProps)).addTo(map);
        setActivePopup(popup);
        recenterParcelPopup(popup);

        try {
            const details = await fetchParcelDetails(item.feature_id);
            if (getSelectedFeatureId() !== item.feature_id || !popup.isOpen()) {
                return;
            }
            renderDetailsIntoPopup(popup, item.feature_id, tileProps, details);
        } catch (err) {
            if (getSelectedFeatureId() !== item.feature_id || !popup.isOpen()) {
                return;
            }
            popup.setHTML(
                createParcelPopupHTML({
                    currentStyle: getCurrentStyle(),
                    title: tileProps.site_address || "Parcel",
                    accent: "#2f6ea9",
                    maxWidth: 360,
                    rows: [
                        { label: "Address", value: tileProps.site_address || "N/A" },
                        { label: "Acres", value: "N/A" },
                        { label: "Details", value: "Unable to load full parcel details" },
                    ],
                }),
            );
            recenterParcelPopup(popup);
        }
    }

    async function handleParcelClick(feature, lngLat) {
        const prevSelected = getSelectedFeatureId();
        if (prevSelected !== null) {
            map.setFeatureState({ source: "parcels", sourceLayer: "parcels", id: prevSelected }, { selected: false });
        }

        const featureId = feature.properties.feature_id;
        setSelectedFeatureId(featureId);
        map.setFeatureState({ source: "parcels", sourceLayer: "parcels", id: featureId }, { selected: true });

        const props = feature.properties;
        const tileProps = { ...props, lat: lngLat.lat, lng: lngLat.lng };
        const popup = new maplibregl.Popup({ focusAfterOpen: false }).setLngLat(lngLat).setHTML(buildParcelLoadingPopup(tileProps)).addTo(map);
        setActivePopup(popup);
        recenterParcelPopup(popup);

        try {
            const details = await fetchParcelDetails(featureId);
            if (getSelectedFeatureId() !== featureId || !popup.isOpen()) {
                return;
            }
            renderDetailsIntoPopup(popup, featureId, tileProps, details);
        } catch (err) {
            if (getSelectedFeatureId() !== featureId || !popup.isOpen()) {
                return;
            }
            popup.setHTML(
                createParcelPopupHTML({
                    currentStyle: getCurrentStyle(),
                    title: tileProps.site_address || "Parcel",
                    accent: "#2f6ea9",
                    maxWidth: 360,
                    rows: [
                        { label: "Address", value: tileProps.site_address || "N/A" },
                        { label: "Acres", value: formatNumber(tileProps.acres) },
                        { label: "Details", value: "Unable to load full parcel details" },
                    ],
                }),
            );
            recenterParcelPopup(popup);
        }
    }

    return {
        openParcelPopupFromSearch,
        handleParcelClick,
        renderDetailsIntoPopup,
    };
}