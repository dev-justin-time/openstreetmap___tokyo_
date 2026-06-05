// osmIntegration.js
import { appConfig } from "./config.js";
import { map } from "./map.js";

/* @tweakable [Nominatim base URL used for place search / reverse geocoding] */
const NOMINATIM_BASE = appConfig.OSM_NOMINATIM_BASE || "https://nominatim.openstreetmap.org";

/* @tweakable [Overpass API base URL used to query OSM features] */
const OVERPASS_BASE = appConfig.OSM_OVERPASS_BASE || "https://overpass-api.de/api/interpreter";

/* @tweakable [Simple debounce ms for client-side request throttling to avoid hitting public API limits] */
let NOMINATIM_DEBOUNCE_MS = appConfig.OSM_NOMINATIM_DEBOUNCE_MS || 600;

/* Internal debounce state */
let _lastNominatimAt = 0;

/**
 * Search places using Nominatim (returns array of results)
 * @param {string} q - query string
 * @param {number} limit - maximum results to return
 */
export async function searchPlace(q, limit = 5) {
  const now = Date.now();
  if (now - _lastNominatimAt < NOMINATIM_DEBOUNCE_MS) {
    await new Promise(r => setTimeout(r, NOMINATIM_DEBOUNCE_MS - (now - _lastNominatimAt)));
  }
  _lastNominatimAt = Date.now();
  const url = `${NOMINATIM_BASE}/search?format=jsonv2&q=${encodeURIComponent(q)}&limit=${Number(limit)}&accept-language=en`;
  const res = await fetch(url, { headers: { "User-Agent": "OSM-Integration/1.0 (+https://example.org)" } });
  if (!res.ok) throw new Error("Nominatim search failed");
  return res.json();
}

/**
 * Reverse geocode to get address / country information for a LatLng-like object
 * @param {{lat:number,lng:number}} latlng
 */
export async function reverseGeocode(latlng) {
  if (!latlng) return null;
  const now = Date.now();
  if (now - _lastNominatimAt < NOMINATIM_DEBOUNCE_MS) {
    await new Promise(r => setTimeout(r, NOMINATIM_DEBOUNCE_MS - (now - _lastNominatimAt)));
  }
  _lastNominatimAt = Date.now();
  const url = `${NOMINATIM_BASE}/reverse?format=jsonv2&lat=${encodeURIComponent(latlng.lat)}&lon=${encodeURIComponent(latlng.lng)}&zoom=10&addressdetails=1`;
  const res = await fetch(url, { headers: { "User-Agent": "OSM-Integration/1.0 (+https://example.org)" } });
  if (!res.ok) return null;
  return res.json();
}

/**
 * Run a simple Overpass query and return elements array
 * @param {string} queryFragment - the inner Overpass query fragment (e.g., `node["amenity"="fuel"](around:10000,35.68,139.76);`)
 */
export async function runOverpassQuery(queryFragment) {
  const q = `[out:json][timeout:25];(${queryFragment});out center;`;
  const res = await fetch(OVERPASS_BASE, { method: "POST", body: q, headers: { "Content-Type": "text/plain" } });
  if (!res.ok) throw new Error("Overpass query failed");
  const j = await res.json();
  return j.elements || [];
}

/**
 * Load a GPX file from a URL and add it to the map as a polyline (lightweight)
 * Note: does not add full waypoint parsing; primarily for quick preview of GPX tracks.
 * @param {string} url
 */
export async function loadGPXToMap(url) {
  try {
    const res = await fetch(url);
    if (!res.ok) throw new Error("GPX fetch failed");
    const text = await res.text();
    const parser = new DOMParser();
    const xml = parser.parseFromString(text, "application/xml");
    const trkpts = Array.from(xml.querySelectorAll("trkpt"));
    if (trkpts.length === 0) return null;
    const coords = trkpts.map(pt => {
      const lat = parseFloat(pt.getAttribute("lat"));
      const lon = parseFloat(pt.getAttribute("lon"));
      return [lat, lon];
    });
    // add lightweight polyline
    const poly = L.polyline(coords, { color: appConfig.GUI_ROUTE_COLOR || "#2b8fff", weight: (appConfig.GUI_ROUTE_WEIGHT_PX || 6) }).addTo(map);
    // fit map to GPX
    try { map.fitBounds(poly.getBounds(), { maxZoom: 15 }); } catch (e) {}
    return poly;
  } catch (e) {
    console.warn("loadGPXToMap failed:", e);
    return null;
  }
}

/* Expose tweakable setters so you can fine tune integration behavior */

/* @tweakable [set Nominatim base URL (useful for proxies or local instances)] */
export function setNominatimBase(url) {
  NOMINATIM_BASE = String(url);
}

/* @tweakable [set Overpass API base URL (useful for proxies or local instances)] */
export function setOverpassBase(url) {
  OVERPASS_BASE = String(url);
}

/* @tweakable [set debounce ms between consecutive Nominatim calls to avoid rate-limits] */
export function setNominatimDebounceMs(ms) {
  NOMINATIM_DEBOUNCE_MS = Number(ms);
}