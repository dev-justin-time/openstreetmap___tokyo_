// ui.js
import { appConfig } from "./config.js";
import { map, driverMarker } from "./map.js";
import { money, fuelLiters, currentSpeedKmph, vehicleSpeedMps, followCar, routeCoords, routeIndexFloat, routeOriginalCoords, CURRENT_COUNTRY_NAME, CURRENT_ROUTE_NAME, CURRENT_ROAD_TYPE, setFollowCar, expandedSteps } from "./simulation.js";
import { distanceToIndexFraction, formatDistanceForDisplay, formatManeuver, getManeuverIconSVG, containsCJK } from "./utils.js";
import { romanizePlaceNameIfNeeded } from "./api.js";

// HUD elements
const hudSpeed = document.getElementById("hud-speed");
const hudFuel = document.getElementById("hud-fuel");
let hudMoney = null; // will be created dynamically
let hudRoute = null; // will be created dynamically
let hudRoad = null; // will be created dynamically
const hudCountry = document.getElementById("hud-country"); // pre-existing
let hudDist = null; // will be created dynamically
let hudEta = null; // will be created dynamically

// HUD visibility state is driven by config; expose setter
export function setInfoPanelVisible(visible) {
  try {
    appConfig.GUI_INFO_PANEL_VISIBLE = Boolean(visible);
    const hudEl = document.getElementById("hud");
    if (hudEl) hudEl.style.display = appConfig.GUI_INFO_PANEL_VISIBLE ? "" : "none";
    const toggle = document.getElementById("hud-toggle");
    if (toggle) toggle.setAttribute("aria-pressed", appConfig.GUI_INFO_PANEL_VISIBLE ? "false" : "true");
  } catch (e) { /* ignore */ }
}

// Turn UI elements
const turnUI = document.getElementById("turn-ui");
const turnTextEl = document.getElementById("turn-text");
const turnDistEl = document.getElementById("turn-dist");
const topTurnUI = document.getElementById("top-turn-ui");
const topTurnTitle = document.getElementById("top-turn-title");
const topTurnPlace = document.getElementById("top-turn-place");
const topTurnIcon = document.getElementById("top-turn-icon");
const topTurnDist = document.getElementById("top-turn-dist");

// Loading overlay elements
const loadingEl = document.getElementById("loading");

export function updateHUD() {
  if (appConfig.GUI_IMPERIAL) {
    const mph = (vehicleSpeedMps * 2.236936).toFixed(0);
    hudSpeed.textContent = `${appConfig.UI_LABELS.speed}: ${mph} ${appConfig.UI_LABELS.mph}`;
    hudFuel.textContent = `${appConfig.UI_LABELS.fuel}: ${(fuelLiters * 0.264172).toFixed(1)} ${appConfig.UI_LABELS.gallons}`;
  } else {
    hudSpeed.textContent = `${appConfig.UI_LABELS.speed}: ${currentSpeedKmph} ${appConfig.UI_LABELS.kmph}`;
    hudFuel.textContent = `${appConfig.UI_LABELS.fuel}: ${fuelLiters.toFixed(1)} ${appConfig.UI_LABELS.liters}`;
  }

  const hud = document.getElementById("hud");
  // Respect global info-panel visibility setting
  if (hud) {
    hud.style.display = appConfig.GUI_INFO_PANEL_VISIBLE ? "" : "none";
  }

  // Create money HUD if not exists
  if (!hudMoney) {
    hudMoney = document.createElement("div");
    hudMoney.id = "hud-money";
    hudMoney.style.fontWeight = "700";
    hud.appendChild(hudMoney);

    // Add follow toggle button
    const followBtn = document.createElement("button");
    followBtn.id = "follow-toggle";
    followBtn.type = "button";
    followBtn.title = "Toggle follow car";
    followBtn.textContent = `${appConfig.UI_LABELS.follow}: ${followCar ? appConfig.UI_LABELS.on : appConfig.UI_LABELS.off}`;
    followBtn.addEventListener("click", () => {
      const desired = !followCar;
      setFollowCar(desired);
      const current = followCar;
      followBtn.textContent = `${appConfig.UI_LABELS.follow}: ${current ? appConfig.UI_LABELS.on : appConfig.UI_LABELS.off}`;
      if (current) {
        const pos = driverMarker.getLatLng();
        map.panTo(pos);
      }
    });
    followBtn.style.cssText = `margin-top:6px;padding:6px 8px;border-radius:6px;border:1px solid rgba(0,0,0,0.06);background:white;cursor:pointer;font-weight:600;`;
    hud.appendChild(followBtn);

    // Create or wire HUD toggle button (small control placed near speed)
    const hudToggle = document.getElementById("hud-toggle");
    if (hudToggle) {
      // set label to indicate state
      hudToggle.textContent = appConfig.GUI_INFO_PANEL_VISIBLE ? appConfig.UI_LABELS.hide : appConfig.UI_LABELS.show;
      hudToggle.setAttribute("aria-pressed", appConfig.GUI_INFO_PANEL_VISIBLE ? "false" : "true");
      hudToggle.addEventListener("click", () => {
        appConfig.GUI_INFO_PANEL_VISIBLE = !appConfig.GUI_INFO_PANEL_VISIBLE;
        hudToggle.textContent = appConfig.GUI_INFO_PANEL_VISIBLE ? appConfig.UI_LABELS.hide : appConfig.UI_LABELS.show;
        hudToggle.setAttribute("aria-pressed", appConfig.GUI_INFO_PANEL_VISIBLE ? "false" : "true");
        const hudEl = document.getElementById("hud");
        if (hudEl) hudEl.style.display = appConfig.GUI_INFO_PANEL_VISIBLE ? "" : "none";
      }, { passive: true });
    }
  }
  hudMoney.textContent = `${appConfig.UI_LABELS.money}: ${appConfig.currency} ${money.toFixed(2)}`;

  // Route name
  if (!hudRoute) {
    hudRoute = document.createElement("div");
    hudRoute.id = "hud-route";
    hudRoute.style.fontWeight = "700";
    hud.appendChild(hudRoute);
  }
  hudRoute.textContent = `${appConfig.UI_LABELS.route}: ${CURRENT_ROUTE_NAME || "—"}`;

  // Road/highway indicator
  if (!hudRoad) {
    hudRoad = document.createElement("div");
    hudRoad.id = "hud-road";
    hudRoad.style.fontWeight = "700";
    hud.appendChild(hudRoad);
  }
  hudRoad.textContent = `${appConfig.UI_LABELS.type}: ${CURRENT_ROAD_TYPE || "—"}`;

  // Country display
  if (hudCountry) { // Pre-existing, just update content and visibility
    hudCountry.textContent = `${appConfig.UI_LABELS.country}: ${CURRENT_COUNTRY_NAME}`;
    hudCountry.style.display = appConfig.GUI_SHOW_COUNTRY ? "" : "none";
  }

  // Remaining distance
  if (appConfig.GUI_SHOW_REMAINING_DISTANCE) {
    if (!hudDist) {
      hudDist = document.createElement("div");
      hudDist.id = "hud-remaining";
      hudDist.style.fontWeight = "700";
      hud.appendChild(hudDist);
    }
    let remainingMeters = 0;
    try {
      if (routeCoords && routeCoords.length >= 2) {
        remainingMeters = distanceToIndexFraction(routeIndexFloat || 0, Math.max(0, routeCoords.length - 1), routeCoords);
      }
    } catch (e) { remainingMeters = 0; }
    hudDist.textContent = `${appConfig.UI_LABELS.remaining}: ${formatDistanceForDisplay(remainingMeters, appConfig.GUI_IMPERIAL)}`;
  } else if (hudDist) {
    hudDist.remove();
    hudDist = null;
  }

  // ETA
  if (appConfig.GUI_SHOW_ETA) {
    if (!hudEta) {
      hudEta = document.createElement("div");
      hudEta.id = "hud-eta";
      hudEta.style.fontWeight = "700";
      hud.appendChild(hudEta);
    }
    let etaText = "—";
    try {
      let remainingMeters = 0;
      if (routeCoords && routeCoords.length >= 2) remainingMeters = distanceToIndexFraction(routeIndexFloat || 0, Math.max(0, routeCoords.length - 1));
      let etaSec = null;
      if (vehicleSpeedMps && vehicleSpeedMps >= appConfig.GUI_ETA_MIN_SPEED_MPS) {
        etaSec = remainingMeters / vehicleSpeedMps;
      } else if (window._routeDurationSec != null) {
        if (routeOriginalCoords && routeOriginalCoords.length >= 2) {
          let totalMeters = 0;
          for (let i = 0; i < routeOriginalCoords.length - 1; i++) {
            totalMeters += map.distance(routeOriginalCoords[i], routeOriginalCoords[i+1]);
          }
          if (totalMeters > 0) {
            const frac = Math.min(1, Math.max(0, remainingMeters / totalMeters));
            etaSec = window._routeDurationSec * frac;
          } else {
            etaSec = window._routeDurationSec;
          }
        } else {
          etaSec = window._routeDurationSec;
        }
      }
      if (etaSec != null && isFinite(etaSec)) {
        const rawMins = Math.max(0, etaSec / 60);
        let mins;
        if (appConfig.GUI_ETA_MINUTES_ROUNDING === "ceil") mins = Math.ceil(rawMins);
        else if (appConfig.GUI_ETA_MINUTES_ROUNDING === "floor") mins = Math.floor(rawMins);
        else mins = Math.round(rawMins);
        mins = Math.max(0, Number.isFinite(mins) ? mins : 0);
        if (mins >= 60) {
          const hrs = Math.floor(mins / 60);
          const rem = mins % 60;
          etaText = rem === 0 ? `${hrs} ${appConfig.UI_LABELS.hr}` : `${hrs} ${appConfig.UI_LABELS.hr} ${rem} ${appConfig.UI_LABELS.min}`;
        } else {
          etaText = `${mins} ${appConfig.UI_LABELS.min}`;
        }
      } else {
        etaText = "—";
      }
    } catch (e) {
      etaText = "—";
    }
    hudEta.textContent = `${appConfig.UI_LABELS.eta}: ${etaText}`;
  } else if (hudEta) {
    hudEta.remove();
    hudEta = null;
  }
}

// update turn UI based on current fractional index
export function updateTurnUI(currentFloatIdx) {
  // `expandedSteps` imported statically from simulation.js
  if (!expandedSteps || expandedSteps.length === 0) {
    if (turnUI) turnUI.setAttribute("aria-hidden", "true");
    if (topTurnUI) topTurnUI.setAttribute("aria-hidden", "true");
    return;
  }

  const base = Math.floor(currentFloatIdx);
  let next = expandedSteps.find(s => s.endIdx > base);
  if (!next) next = expandedSteps[expandedSteps.length - 1];
  const instruction = formatManeuver(next.step);
  const dist = distanceToIndexFraction(currentFloatIdx, next.endIdx, routeCoords);

  const formattedDist = (typeof appConfig.TOP_TURN_KM_THRESHOLD === "number" && dist >= appConfig.TOP_TURN_KM_THRESHOLD)
    ? `${(dist / 1000).toFixed(1)} ${appConfig.UI_LABELS.km}`
    : `${dist} ${appConfig.UI_LABELS.m}`;

  if (turnUI) turnUI.setAttribute("aria-hidden", "false");
  if (turnTextEl) turnTextEl.textContent = formattedDist; // Distance for bottom UI
  if (turnDistEl) turnDistEl.textContent = instruction; // Instruction for bottom UI

  try {
    if (topTurnUI) topTurnUI.setAttribute("aria-hidden", "false");
    if (topTurnTitle) topTurnTitle.textContent = instruction;
    let placeText = next.step.name || (next.step.ref ? next.step.ref : "");
    if (placeText && appConfig.TOP_TURN_SHOW_PLACE) {
      let display = placeText;
      if (containsCJK(placeText) && next.step.maneuver && next.step.maneuver.location) {
        romanizePlaceNameIfNeeded(placeText, next.step.maneuver.location).then((r) => {
          const short = r.length > appConfig.TOP_TURN_PLACE_MAX_CHARS ? r.slice(0, appConfig.TOP_TURN_PLACE_MAX_CHARS - 1) + "…" : r;
          try {
            topTurnPlace.textContent = `At: ${short}`;
            topTurnPlace.style.display = "";
          } catch (e) { console.warn("Failed to update topTurnPlace", e); }
        });
      }
      const short = display.length > appConfig.TOP_TURN_PLACE_MAX_CHARS ? display.slice(0, appConfig.TOP_TURN_PLACE_MAX_CHARS - 1) + "…" : display;
      topTurnPlace.textContent = `At: ${short}`;
      topTurnPlace.style.display = "";
    } else {
      topTurnPlace.style.display = "none";
    }
    if (topTurnDist) topTurnDist.textContent = formattedDist;

    const m = next.step.maneuver || {};
    const svg = getManeuverIconSVG(m.modifier, m.type, appConfig.TOP_TURN_ICON_SIZE_PX);
    if (topTurnIcon) topTurnIcon.innerHTML = svg;
    const bottomIconEl = document.querySelector(".turn-ui .turn-icon");
    if (bottomIconEl) bottomIconEl.innerHTML = svg;
  } catch (e) { /* non-critical UI failure — ignore */ }
}

export function showLoading(msg = "Calculating route...") {
  if (!loadingEl) return;
  loadingEl.querySelector(".loading-text").textContent = msg;
  loadingEl.setAttribute("aria-hidden", "false");
}
export function hideLoading() {
  if (!loadingEl) return;
  loadingEl.setAttribute("aria-hidden", "true");
}