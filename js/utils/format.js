export function formatNumber(num) {
    if (num === null || num === undefined || num === "") return "N/A";
    return Number(num).toLocaleString("en-US");
}

export function formatCurrency(num) {
    if (num === null || num === undefined || num === "") return "N/A";
    const n = Number(num);
    if (!Number.isFinite(n)) return "N/A";
    return n.toLocaleString("en-US", { style: "currency", currency: "USD" });
}

export function formatLocalDateTime(value) {
    if (!value) return "N/A";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "N/A";
    return date.toLocaleDateString();
}

export function escapeHtml(str) {
    if (str === null || str === undefined) return "";
    return String(str)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#39;");
}

export function normalizeAddressText(value) {
    if (value === null || value === undefined) return "";
    return String(value)
        .replace(/\r?\n+/g, ", ")
        .replace(/\s+/g, " ")
        .replace(/\s*,\s*/g, ", ")
        .replace(/,{2,}/g, ",")
        .replace(/\s+,/g, ",")
        .trim()
        .replace(/,+$/, "");
}

export function toNumberOrNull(value) {
    if (value === null || value === undefined || value === "") return null;
    const n = Number(value);
    return Number.isFinite(n) ? n : null;
}

export async function copyTextToClipboard(text) {
    const value = String(text || "").trim();
    if (!value) return false;

    try {
        if (navigator.clipboard && window.isSecureContext) {
            await navigator.clipboard.writeText(value);
            return true;
        }
    } catch (_) {
        // Fall through to legacy copy path.
    }

    try {
        const textarea = document.createElement("textarea");
        textarea.value = value;
        textarea.setAttribute("readonly", "");
        textarea.style.position = "fixed";
        textarea.style.top = "-9999px";
        textarea.style.left = "-9999px";
        document.body.appendChild(textarea);
        textarea.select();
        const copied = document.execCommand("copy");
        document.body.removeChild(textarea);
        return copied;
    } catch (_) {
        return false;
    }
}
