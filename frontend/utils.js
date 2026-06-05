// utils.js
import { map } from "./map.js";
import { appConfig } from "./frontend/config.js";

// @tweakable [use remote romanization via Nominatim (true) or return original name immediately (false)]
const UTIL_USE_REMOTE_ROMANIZE = true;

// Utility: detect CJK characters quickly
export function containsCJK(s) {
  return /[\u3000-\u30FF\u4E00-\u9FFF\uAC00-\uD7AF]/.test(s || "");
}

export async function romanizePlaceNameIfNeeded(name, latlng) {
  if (!UTIL_USE_REMOTE_ROMANIZE) return name;
  try {
    const api = await import('./api.js');
    if (typeof api.romanizePlaceNameIfNeeded === 'function') {
      return await api.romanizePlaceNameIfNeeded(name, latlng);
    }
  } catch (e) {
    console.warn("romanizePlaceNameIfNeeded fallback:", e);
  }
  return name;
}

export function formatDistanceForDisplay(meters, isImperial) {
  if (isImperial) {
    const miles = meters * 0.000621371;
    if (miles >= 0.1) return `${miles.toFixed(2)} mi`;
    const feet = meters * 3.28084;
    return `${Math.round(feet)} ft`;
  } else {
    if (meters >= 1000) return `${(meters / 1000).toFixed(1)} km`;
    return `${Math.round(meters)} m`;
  }
}

// small helper to format a maneuver into readable text
export function formatManeuver(step) {
  const m = step.maneuver || {};
  const parts = [];
  if (m.modifier) parts.push(m.modifier);
  if (m.type) parts.unshift(m.type);
  if (step.name) parts.push(step.name);
  if (parts.length === 0) return "Continue straight";
  return parts.join(" ");
}

// compute remaining distance (meters) from current fractional index to the maneuver end index
export function distanceToIndexFraction(currentFloatIdx, targetIdx, routeCoords) {
  if (!routeCoords || routeCoords.length < 2) return 0;
  let dist = 0;
  const base = Math.floor(currentFloatIdx);
  const frac = currentFloatIdx - base;
  const a = routeCoords[base];
  const b = routeCoords[Math.min(base + 1, routeCoords.length - 1)];
  const segRemaining = map.distance(a, b) * (1 - frac);
  if (base >= targetIdx) return 0;
  dist += segRemaining;
  for (let i = base + 1; i < targetIdx; i++) {
    const p = routeCoords[i];
    const q = routeCoords[Math.min(i + 1, routeCoords.length - 1)];
    dist += map.distance(p, q);
  }
  return Math.round(dist);
}

// build expanded steps mapping for turn info
export function buildStepsIndex(routeSteps, coordsForRoute) {
  const expandedSteps = [];
  let cursor = 0;
  for (const step of routeSteps) {
    const len = step.geometry ? step.geometry.coordinates.length : 0;
    const startIdx = cursor;
    const endIdx = Math.max(cursor + Math.max(0, len - 1), cursor);
    expandedSteps.push({ step, startIdx, endIdx });
    cursor = endIdx;
  }
  return expandedSteps;
}

export function getManeuverIconSVG(modifier, type, size) {
  const s = size;
  const stroke = "#111";
  const common = {
    straight: `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M12 19V5" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M5 12l7-7 7 7" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
    left: `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M19 12H7" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M10 5L3 12l7 7" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
    right: `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M5 12h12" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M14 5l7 7-7 7" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
    slight_left: `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M19 12H9" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M9 5L3 12l6 7" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
    slight_right: `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M5 12h10" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M15 5l6 7-6 7" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
    sharp_left: `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M19 12H8" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M8 5L3 12l5 7" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
    sharp_right: `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M5 12h11" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M16 5l5 7-5 7" stroke="${stroke}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`
  };
  let key = (modifier || "").toLowerCase().replace(/\s+/g, "_");

  if (appConfig.GUI_INVERT_TURN_DIRECTIONS) {
    if (key === "left") key = "right";
    else if (key === "right") key = "left";
    else if (key === "slight_left") key = "slight_right";
    else if (key === "slight_right") key = "slight_left";
    else if (key === "sharp_left") key = "sharp_right";
    else if (key === "sharp_right") key = "sharp_left";
  }

  if (common[key]) return common[key];
  if (type && String(type).toLowerCase().includes("roundabout")) {
    return `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><circle cx="12" cy="12" r="7" stroke="${stroke}" stroke-width="2"/><path d="M12 5v4" stroke="${stroke}" stroke-width="2" stroke-linecap="round"/></svg>`;
  }
  return common.straight;
}