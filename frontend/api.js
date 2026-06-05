// api.js
import { appConfig } from "./config.js";
import { containsCJK } from "./utils.js";

/* Nominatim debounce state (shared across all Nominatim calls) */
let _lastNominatimAt = 0;
let NOMINATIM_DEBOUNCE_MS = appConfig.OSM_NOMINATIM_DEBOUNCE_MS || 600;

async function _nominatimDebounce() {
  const now = Date.now();
  if (now - _lastNominatimAt < NOMINATIM_DEBOUNCE_MS) {
    await new Promise(r => setTimeout(r, NOMINATIM_DEBOUNCE_MS - (now - _lastNominatimAt)));
  }
  _lastNominatimAt = Date.now();
}

const NOMINATIM_UA = "OSM-Integration/1.0 (+https://example.org)";

// Utility: fetch route from OSRM public server (driving profile)
export async function fetchRoute(from, to) {
  const url = `${appConfig.OSRM_BASE}${from.lng},${from.lat};${to.lng},${to.lat}?overview=full&geometries=geojson&steps=true`;
  const res = await fetch(url);
  if (!res.ok) throw new Error("Routing failed");
  const data = await res.json();
  if (!data.routes || !data.routes[0]) throw new Error("No route");
  return data.routes[0];
}

// Utility: query Overpass for gas stations within radius (meters)
export async function fetchGasStations(center, radius = 10000) {
  const q = `[out:json][timeout:25];
    (
      node["amenity"="fuel"](around:${radius},${center.lat},${center.lng});
      way["amenity"="fuel"](around:${radius},${center.lat},${center.lng});
      relation["amenity"="fuel"](around:${radius},${center.lat},${center.lng});
    );
    out center;`;
  const url = appConfig.OSM_OVERPASS_BASE;
  const res = await fetch(url, { method: "POST", body: q });
  if (!res.ok) throw new Error("Overpass query failed");
  const data = await res.json();
  return data.elements || [];
}

// Utility: fetch multiple route alternatives from OSRM and return the best (fastest) one
export async function fetchRouteAlternatives(from, to, maxAlternatives = 3) {
  const url = `${appConfig.OSRM_BASE}${from.lng},${from.lat};${to.lng},${to.lat}?overview=full&geometries=geojson&steps=true&alternatives=${Math.max(1, Math.min(10, maxAlternatives))}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error("Routing failed (alternatives)");
  const data = await res.json();
  if (!data.routes || data.routes.length === 0) throw new Error("No route alternatives");
  let best = data.routes[0];
  for (const r of data.routes) {
    const scoreR = (r.duration || 1) * (appConfig.TURBO_STRICTNESS) + (r.distance || 0) * (1 - appConfig.TURBO_STRICTNESS);
    const scoreBest = (best.duration || 1) * (appConfig.TURBO_STRICTNESS) + (best.distance || 0) * (1 - appConfig.TURBO_STRICTNESS);
    if (scoreR < scoreBest) best = r;
  }
  return best;
}

// Search places using Nominatim
export async function searchPlace(q, limit = 5) {
  await _nominatimDebounce();
  const url = `${appConfig.OSM_NOMINATIM_BASE}/search?format=jsonv2&q=${encodeURIComponent(q)}&limit=${Number(limit)}&accept-language=en`;
  const res = await fetch(url, { headers: { "User-Agent": NOMINATIM_UA } });
  if (!res.ok) throw new Error("Nominatim search failed");
  return res.json();
}

// Reverse geocode to get country name
export async function reverseGeocodeCountry(latlng) {
  await _nominatimDebounce();
  const url = `${appConfig.OSM_NOMINATIM_BASE}/reverse?format=jsonv2&lat=${latlng.lat}&lon=${latlng.lng}&zoom=1&addressdetails=0`;
  const res = await fetch(url, { headers: { "User-Agent": NOMINATIM_UA } });
  if (!res.ok) return null;
  const j = await res.json();
  return j.address && j.address.country ? j.address.country : null;
}

// Try to romanize a place name by re-querying Nominatim in English; returns original if not found
export async function romanizePlaceNameIfNeeded(name, latlng) {
  try {
    if (!name) return name;
    if (!containsCJK(name)) return name;
    if (latlng && latlng.length === 2) {
      await _nominatimDebounce();
      const url = `${appConfig.OSM_NOMINATIM_BASE}/reverse?format=jsonv2&lat=${latlng[0]}&lon=${latlng[1]}&accept-language=en`;
      const res = await fetch(url, { headers: { "User-Agent": NOMINATIM_UA } });
      if (!res.ok) return name;
      const j = await res.json();
      const cand = (j && (j.name || j.display_name)) ? (j.name || j.display_name) : name;
      return cand;
    } else {
      return name;
    }
  } catch (e) {
    return name;
  }
}