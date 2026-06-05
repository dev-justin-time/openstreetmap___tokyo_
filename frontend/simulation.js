// simulation.js
import * as L from "leaflet";
import { appConfig } from "./config.js";
import { map, driverMarker } from "./map.js";
import { updateHUD, updateTurnUI } from "./ui.js";
import { showConfirmModal } from "./confirmModal.js";
import { buildStepsIndex, distanceToIndexFraction } from "./utils.js";
import { reverseGeocodeCountry } from "./api.js";

// Simulation state
export let routeGeo = null;
export let routeCoords = [];
export let routeOriginalCoords = []; // store immutable original coords for trimming
export let routeIndex = 0;
export let routeIndexFloat = 0; // fractional position along coords for smooth movement
export let animId = null;
export let currentSpeedKmph = 0;
export let vehicleSpeedMps = 0; // actual current vehicle speed (m/s) for realistic accel/decel
export let money = 200; // starting funds
export let currency = "JPY";
export let fuelPricePerLiter = 200; // default JPY-ish value
export let fuelLiters = appConfig.FUEL_TANK_CAPACITY * 0.6; // start with ~60% fuel by default
export let followCar = false;
export let CURRENT_ROUTE_NAME = "";
export let CURRENT_ROAD_TYPE = ""; // "road" | "highway" | "unknown"
export let gasMarkers = [];

// Incident state
export let _crashMarker = null;
export let _crashed = false;

// Country display state
export let CURRENT_COUNTRY_NAME = "—";
let _countryReverseGeocodeTimer = null;
let _lastCountryGeocodeLatLng = null;

/* @tweakable [programmatically set the displayed current country name in the HUD] */
export function setCurrentCountryName(name) {
  CURRENT_COUNTRY_NAME = name || "—";
  // trigger HUD update in a best-effort, dynamic import to avoid cycles
  import('./ui.js').then(m => m.updateHUD()).catch(()=>{});
}

/* @tweakable [toggle map follow mode (pan-to-driver) programmatically] */
export function setFollowCar(val) {
  followCar = Boolean(val);
}

/* @tweakable [programmatically set the current route name shown in HUD] */
export function setCurrentRouteName(name) {
  CURRENT_ROUTE_NAME = name || "";
  import('./ui.js').then(m => m.updateHUD()).catch(()=>{});
}

/* @tweakable [programmatically set the fuel level (liters) shown in the HUD] */
export function setFuelLiters(l) {
  try {
    fuelLiters = Math.max(0, Number(l) || 0);
  } catch (e) {
    fuelLiters = Math.max(0, +(l || 0));
  }
  import('./ui.js').then(m => m.updateHUD()).catch(()=>{});
}

/* @tweakable [programmatically set the money balance shown in the HUD] */
export function setMoney(m) {
  try {
    money = Number(m) || 0;
  } catch (e) {
    money = +(m || 0);
  }
  import('./ui.js').then(mo => mo.updateHUD()).catch(()=>{});
}

/* @tweakable [programmatically set the current road type shown in HUD (e.g., "highway"|"road")] */
export function setCurrentRoadType(t) {
  CURRENT_ROAD_TYPE = t || "";
  import('./ui.js').then(m => m.updateHUD()).catch(()=>{});
}

/* @tweakable [default currency used when country code is unknown] */
const DEFAULT_CURRENCY = "USD";

/* @tweakable [basic mapping of country code -> currency used to choose display currency] */
const COUNTRY_CODE_TO_CURRENCY = {
  JP: "JPY",
  US: "USD",
  GB: "GBP",
  KR: "KRW",
  CN: "CNY",
  DE: "EUR",
  FR: "EUR",
  // ...existing mapping can be extended via tweakables if needed
};

/* @tweakable [set the displayed currency based on ISO country code (e.g. "US" -> "USD")] */
export function setCurrencyForCountryCode(cc) {
  try {
    if (!cc || typeof cc !== "string") {
      currency = DEFAULT_CURRENCY;
      return;
    }
    const code = cc.toUpperCase();
    currency = COUNTRY_CODE_TO_CURRENCY[code] || DEFAULT_CURRENCY;
    // Trigger HUD update
    import('./ui.js').then(m => m.updateHUD()).catch(()=>{});
  } catch (e) {
    console.warn("setCurrencyForCountryCode failed:", e);
    currency = DEFAULT_CURRENCY;
  }
}

// Add small helper to get effective cruise limit
export function getEffectiveCruiseKmph() {
  return appConfig.GUI_NO_SPEED_LIMIT ? 10000 : appConfig.GUI_MAX_CRUISE_KMPH;
}

// Compute speed (km/h) per segment using step duration/distance as a fallback for speed-limit
export function computeSegmentSpeedMetersPerSecond(seg) {
  let baseMps;
  if (seg.duration && seg.distance && seg.duration > 0) {
    baseMps = seg.distance / seg.duration;
  } else {
    baseMps = (50 * 1000) / 3600;
  }
  try {
    const segKmH = baseMps * 3.6;
    const segLen = seg.distance || 0;
    const name = (seg.name || "").toLowerCase();
    const ref = (seg.ref || "").toLowerCase();
    const motorwayTokens = ["motorway", "高速", "expressway", "highway", "autobahn", "autoroute", "route", "i-", "shuto", "route"];
    const hasMotorwayToken = motorwayTokens.some(t => name.includes(t) || ref.includes(t));
    const detectFast = segKmH >= appConfig.HIGHWAY_DETECT_MIN_KMPH && segLen >= appConfig.HIGHWAY_MIN_SEGMENT_LENGTH_M;
    if (!appConfig.GUI_DISABLE_SMART_SPEED && (hasMotorwayToken || detectFast)) {
      const boosted = baseMps * appConfig.HIGHWAY_SPEED_MULTIPLIER * appConfig.GUI_SPEED_REALISM_MULTIPLIER;
      const capMps = (Math.min(getEffectiveCruiseKmph(), 10000) * 1000) / 3600;
      const minHighwayMps = (Math.max(0, appConfig.GUI_HIGHWAY_MIN_KMPH) * 1000) / 3600;
      const candidate = Math.min(Math.max(boosted, baseMps, minHighwayMps), capMps);
      return candidate;
    }
  } catch (e) {
    // ignore and return base
  }
  return baseMps * appConfig.GUI_SPEED_REALISM_MULTIPLIER;
}

// store expanded steps mapping so we can show next-turn info
export let expandedSteps = []; // each item: { step, startIdx, endIdx }

// Animate driver along route coordinates — improved fractional interpolation and stable movement
export function startAnimation(geojson, routeSteps) {
  if (!geojson || !geojson.coordinates || geojson.coordinates.length === 0) return;
  routeGeo = geojson;
  routeCoords = geojson.coordinates.map(c => [c[1], c[0]]);
  routeOriginalCoords = routeCoords.slice(); // keep immutable copy for trimming
  routeIndex = 0;
  routeIndexFloat = 0; // fractional position along coords for smooth movement
  vehicleSpeedMps = 0; // actual current vehicle speed (m/s) for realistic accel/decel
  if (animId) cancelAnimationFrame(animId);

  const segmentSpeeds = new Array(Math.max(1, routeCoords.length - 1)).fill((50 * 1000) / 3600);
  let coordCursor = 0;
  for (const step of routeSteps) {
    const stepLen = step.geometry ? step.geometry.coordinates.length : 0;
    const speedMps = computeSegmentSpeedMetersPerSecond(step);
    for (let i = 0; i < Math.max(1, stepLen - 1); i++) {
      const idx = coordCursor + i;
      if (idx >= 0 && idx < segmentSpeeds.length) segmentSpeeds[idx] = speedMps;
    }
    coordCursor += Math.max(0, stepLen - 1);
  }

  expandedSteps = buildStepsIndex(routeSteps || [], routeCoords);

  let lastTime = null;

  function tick(t) {
    if (!lastTime) lastTime = t;
    const dt = (t - lastTime) / 1000; // seconds
    lastTime = t;

    if (routeCoords.length < 2) {
      currentSpeedKmph = 0;
      updateHUD();
      cancelAnimationFrame(animId);
      animId = null;
      return;
    }

    const idxFloor = Math.floor(routeIndexFloat);
    const idx = Math.min(idxFloor, routeCoords.length - 2);
    const nextIdx = idx + 1;

    const curr = routeCoords[idx];
    const next = routeCoords[nextIdx];
    const segDist = map.distance(curr, next); // meters
    const speedLimitMps = (Math.min(getEffectiveCruiseKmph(), 10000) * 1000) / 3600;
    const baseTarget = Math.min(segmentSpeeds[idx] || ((50 * 1000) / 3600), speedLimitMps);
    let targetSpeedMps = baseTarget;

    try {
      targetSpeedMps = Math.min(targetSpeedMps * appConfig.GUI_SPEED_REALISM_MULTIPLIER, speedLimitMps);
    } catch (e) { /* fallback if tweakable missing */ }

    // Curvature-based slow down
    try {
      const lookAhead = 3;
      let curvature = 0;
      const aIdx = Math.max(0, idx - 1);
      for (let k = aIdx; k <= Math.min(routeCoords.length - 3, idx + lookAhead); k++) {
        const p1 = routeCoords[k];
        const p2 = routeCoords[k + 1];
        const p3 = routeCoords[k + 2];
        if (!p1 || !p2 || !p3) continue;
        const v1 = [p2[0] - p1[0], p2[1] - p1[1]];
        const v2 = [p3[0] - p2[0], p3[1] - p2[1]];
        const dot = v1[0] * v2[0] + v1[1] * v2[1];
        const mag1 = Math.hypot(v1[0], v1[1]) || 1e-6;
        const mag2 = Math.hypot(v2[0], v2[1]) || 1e-6;
        const cos = Math.max(-1, Math.min(1, dot / (mag1 * mag2)));
        const angle = Math.acos(cos);
        curvature += angle;
      }
      curvature = curvature / (lookAhead + 1);
      const curvatureFactor = 1 - Math.min(0.7, (curvature * appConfig.GUI_CURVATURE_SENSITIVITY));
      targetSpeedMps = targetSpeedMps * Math.max(0.35, curvatureFactor);
    } catch (e) { /* ignore curvature failures */ }

    // Micro-brake events
    if (!appConfig.GUI_INSTANT_ACCEL) {
      const estimatedKmMoved = (vehicleSpeedMps * dt) / 1000;
      const microChance = Math.min(1, appConfig.GUI_MICRO_BRAKE_PROB_PER_KM * estimatedKmMoved);
      if (Math.random() < microChance) {
        const dur = appConfig.GUI_MICRO_BRAKE_DURATION_SEC.min + Math.random() * (appConfig.GUI_MICRO_BRAKE_DURATION_SEC.max - appConfig.GUI_MICRO_BRAKE_DURATION_SEC.min);
        const originalTarget = targetSpeedMps;
        targetSpeedMps = Math.max(1, targetSpeedMps * (0.3 + Math.random() * 0.5));
        driverMarker._microBrakeRestoreTime = (performance.now() / 1000) + dur;
        driverMarker._microBrakeTarget = originalTarget;
      } else if (driverMarker._microBrakeRestoreTime) {
        const nowSec = performance.now() / 1000;
        if (nowSec < driverMarker._microBrakeRestoreTime && driverMarker._microBrakeTarget) {
          const tleft = driverMarker._microBrakeRestoreTime - nowSec;
          const blend = Math.min(1, Math.max(0, 1 - (tleft / appConfig.GUI_MICRO_BRAKE_DURATION_SEC.max)));
          targetSpeedMps = targetSpeedMps * (1 - blend) + driverMarker._microBrakeTarget * blend;
        } else {
          driverMarker._microBrakeRestoreTime = null;
          driverMarker._microBrakeTarget = null;
        }
      }
    }

    // Natural speed variation
    if (!appConfig.GUI_INSTANT_ACCEL) {
      const noise = (Math.random() * 2 - 1) * appConfig.GUI_NATURAL_SPEED_VARIATION;
      targetSpeedMps = Math.max(0.1, targetSpeedMps * (1 + noise));
    }

    // Realistic accel/decel
    if (appConfig.GUI_INSTANT_ACCEL) {
      vehicleSpeedMps = targetSpeedMps;
    } else if (vehicleSpeedMps < targetSpeedMps) {
      vehicleSpeedMps = Math.min(targetSpeedMps, vehicleSpeedMps + appConfig.MAX_ACCEL * dt);
    } else {
      vehicleSpeedMps = Math.max(targetSpeedMps, vehicleSpeedMps - appConfig.MAX_DECEL * dt);
    }

    const moveMeters = vehicleSpeedMps * dt;
    const kmMoved = moveMeters / 1000;

    if (segDist > 0) {
      const fracAdvance = moveMeters / segDist;
      routeIndexFloat = Math.min(routeCoords.length - 1, routeIndexFloat + fracAdvance);
      routeIndex = Math.floor(routeIndexFloat);
    }

    if (routeIndexFloat >= routeCoords.length - 1) {
      driverMarker.setLatLng(routeCoords[routeCoords.length - 1]);
      currentSpeedKmph = 0;
      updateHUD();
      cancelAnimationFrame(animId);
      animId = null;
      updateCountryDisplay(driverMarker.getLatLng());
      return;
    }

    const baseIdx = Math.floor(routeIndexFloat);
    const frac = routeIndexFloat - baseIdx;
    const a = routeCoords[baseIdx];
    const b = routeCoords[Math.min(baseIdx + 1, routeCoords.length - 1)];
    const lat = a[0] + (b[0] - a[0]) * frac;
    const lng = a[1] + (b[1] - a[1]) * frac;
    const currentDriverLatLng = L.latLng(lat, lng);
    driverMarker.setLatLng(currentDriverLatLng);

    updateCountryDisplay(currentDriverLatLng);
    try { updateTurnUI(routeIndexFloat); } catch (e) { /* ignore UI errors */ }

    if (window._routeLine && window._routeLine.getLatLngs) {
      try {
        const allLatLngs = routeOriginalCoords.map(c => L.latLng(c[0], c[1]));
        const trimIndex = Math.min(Math.max(0, baseIdx), allLatLngs.length - 1);
        const newLatLngs = [L.latLng(lat, lng)].concat(allLatLngs.slice(trimIndex + 1));
        window._routeLine.setLatLngs(newLatLngs);
      } catch (e) { /* ignore any polyline errors */ }
    }

    if (!appConfig.GUI_INFINITE_FUEL) {
      fuelLiters = Math.max(0, fuelLiters - (appConfig.FUEL_CONSUMPTION_BASE * kmMoved / 100));
    }

    if (appConfig.GUI_INCIDENTS_ENABLED && !_crashed && kmMoved > 0) {
      const chance = Math.min(1, appConfig.INCIDENT_PROBABILITY_PER_KM * kmMoved);
      if (Math.random() < chance) {
        try {
          handleCrashAt([lat, lng]);
          return;
        } catch (e) {
          console.warn("Crash handling failed", e);
        }
      }
    }

    currentSpeedKmph = Math.round(vehicleSpeedMps * 3.6);
    updateHUD();

    if (followCar) {
      map.panTo([lat, lng], { animate: true, duration: 0.2 });
    }

    if (fuelLiters <= 0) {
      currentSpeedKmph = 0;
      updateHUD();
      cancelAnimationFrame(animId);
      animId = null;
      return;
    }

    animId = requestAnimationFrame(tick);
  }

  animId = requestAnimationFrame(tick);
}

// helper to create/remove crash marker
export function placeCrashMarker(latlng) {
  try {
    if (_crashMarker) {
      try { map.removeLayer(_crashMarker); } catch (e) { /* ignore */ }
      _crashMarker = null;
    }
    const icon = L.divIcon({
      html: '🚨',
      className: 'crash-emoji',
      iconSize: [24, 24],
      iconAnchor: [12, 12],
    });
    _crashMarker = L.marker(latlng, { icon }).addTo(map);
  } catch (e) {
    console.warn("Crash marker failed:", e);
  }
}

export async function handleCrashAt(latlng) {
  _crashed = true;
  if (animId) {
    cancelAnimationFrame(animId);
    animId = null;
  }
  currentSpeedKmph = 0;
  updateHUD();
  placeCrashMarker(latlng);

  const towCost = Math.min(money, 20 + Math.round(Math.random() * 80));
  const fuelLoss = Math.min(fuelLiters, 2 + Math.random() * 6);
  money = Math.max(0, +(money - towCost).toFixed(2));
  fuelLiters = Math.max(0, +(fuelLiters - fuelLoss).toFixed(2));
  updateHUD();

  let prevTitle, prevBody;
  const confirmModalEl = document.getElementById("confirm-modal");
  if (confirmModalEl) {
    const tEl = confirmModalEl.querySelector(".confirm-title");
    const bEl = confirmModalEl.querySelector(".confirm-body");
    if (tEl) { prevTitle = tEl.textContent; tEl.textContent = "Car incident"; }
    if (bEl) { prevBody = bEl.textContent; bEl.textContent = `A collision occurred. Tow cost ${currency} ${towCost.toFixed(2)}. Pay tow and resume?`; }
  } else {
    console.warn("Confirm modal missing – cannot prompt user to tow.");
    return;
  }

  const ok = await showConfirmModal();

  if (confirmModalEl) {
    const tEl2 = confirmModalEl.querySelector(".confirm-title");
    const bEl2 = confirmModalEl.querySelector(".confirm-body");
    if (tEl2) tEl2.textContent = prevTitle;
    if (bEl2) bEl2.textContent = prevBody;
  }

  if (ok) {
    try { if (_crashMarker) map.removeLayer(_crashMarker); } catch (e) { /* ignore */ }
    _crashMarker = null;
    _crashed = false;
    if (!animId) {
      requestAnimationFrame((t) => {
        if (routeGeo) {
          startAnimation(routeGeo, expandedSteps.map(e => e.step));
        }
      });
    }
  }
}

// Function to handle country name updates (debounced)
export function updateCountryDisplay(currentLatLng) {
  if (!appConfig.GUI_SHOW_COUNTRY) {
    if (_countryReverseGeocodeTimer) clearTimeout(_countryReverseGeocodeTimer);
    return;
  }

  if (_lastCountryGeocodeLatLng && map.distance(currentLatLng, _lastCountryGeocodeLatLng) < appConfig.COUNTRY_REVERSE_GEOCODE_MIN_DIST_M) {
    return;
  }

  if (_countryReverseGeocodeTimer) {
    clearTimeout(_countryReverseGeocodeTimer);
  }

  _countryReverseGeocodeTimer = setTimeout(async () => {
    try {
      _lastCountryGeocodeLatLng = currentLatLng;
      const countryName = await reverseGeocodeCountry(currentLatLng);
      if (countryName && CURRENT_COUNTRY_NAME !== countryName) {
        CURRENT_COUNTRY_NAME = countryName;
        updateHUD();
      }
    } catch (e) {
      console.warn("Failed to reverse geocode country:", e);
    } finally {
      _countryReverseGeocodeTimer = null;
    }
  }, appConfig.COUNTRY_REVERSE_GEOCODE_DEBOUNCE_MS);
}

// Logic for gas stations
export function placeGasStations(center) {
  // This needs `fetchGasStations` from `api.js`
  import('./api.js').then(({ fetchGasStations }) => {
    fetchGasStations(center, 10000).then(results => {
      gasMarkers.forEach(m => { try { map.removeLayer(m); } catch { } });
      gasMarkers.length = 0; // Clear previous markers

      results.forEach((el) => {
        let lat = el.lat || (el.center && el.center.lat);
        let lon = el.lon || (el.center && el.center.lon);
        if (!lat || !lon) return;
        const icon = L.divIcon({
          html: "⛽",
          className: "gas-emoji",
          iconSize: [20, 20],
          iconAnchor: [10, 10],
        });
        const m = L.marker([lat, lon], { icon }).addTo(map);
        m.on("click", () => {
          const needed = Math.max(0, appConfig.FUEL_TANK_CAPACITY - fuelLiters);
          if (needed <= 0) return;
          const affordableLiters = Math.floor((money / fuelPricePerLiter) * 100) / 100;
          const buy = Math.min(needed, affordableLiters);
          if (buy <= 0) return;
          const cost = +(buy * fuelPricePerLiter).toFixed(2);
          fuelLiters = Math.min(appConfig.FUEL_TANK_CAPACITY, fuelLiters + buy);
          money = Math.max(0, +(money - cost).toFixed(2));
          updateHUD();
        });
        gasMarkers.push(m);
      });
    }).catch(e => {
      console.warn("Gas station fetch failed:", e);
    });
  });
}