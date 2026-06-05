// gui.js
import { appConfig } from "./config.js";
import { updateHUD, updateTurnUI, showLoading, hideLoading } from "./ui.js";
import { setCurrencyForCountryCode, fuelLiters, money, setFuelLiters, setMoney, setCurrentCountryName, setFollowCar, setCurrentRouteName, setCurrentRoadType, startAnimation, getEffectiveCruiseKmph, updateCountryDisplay, gasMarkers } from "./simulation.js";
import { driverMarker, map, markerLayer, addMarker } from "./map.js";
import { fetchRouteAlternatives, fetchRoute, searchPlace } from "./api.js";
import { showConfirmModal } from "./confirmModal.js";

export function initGUI() {
  const gui = document.getElementById("gui");
  const guiToggle = document.getElementById("gui-toggle");
  const guiPanel = document.getElementById("gui-panel");
  const cruiseRange = document.getElementById("cruise-speed");
  const cruiseVal = document.getElementById("cruise-speed-val");
  const instantAccCb = document.getElementById("instant-acc");
  const noSpeedLimitCb = document.getElementById("no-speed-limit");
  const disableSmartCb = document.getElementById("disable-smart-speed");
  const infiniteFuelCb = document.getElementById("infinite-fuel");
  const teleportInput = document.getElementById("teleport");
  const teleportGo = document.getElementById("teleport-go");
  const invertTurnsCb = document.getElementById("invert-turns");
  const incidentsCb = document.getElementById("incidents-enabled");
  const incidentProbRange = document.getElementById("incident-prob");
  const incidentProbVal = document.getElementById("incident-prob-val");
  const imperialCb = document.getElementById("imperial-mode");
  const turboAltsRange = document.getElementById("turbo-alts");
  const turboAltsVal = document.getElementById("turbo-alts-val");

  // Initial GUI values from config
  if (cruiseRange) {
    cruiseRange.value = appConfig.GUI_MAX_CRUISE_KMPH;
    cruiseVal.textContent = `${appConfig.GUI_MAX_CRUISE_KMPH} km/h`;
  }
  if (instantAccCb) instantAccCb.checked = appConfig.GUI_INSTANT_ACCEL;
  if (noSpeedLimitCb) noSpeedLimitCb.checked = appConfig.GUI_NO_SPEED_LIMIT;
  if (disableSmartCb) disableSmartCb.checked = appConfig.GUI_DISABLE_SMART_SPEED;
  if (infiniteFuelCb) infiniteFuelCb.checked = appConfig.GUI_INFINITE_FUEL;
  if (invertTurnsCb) invertTurnsCb.checked = appConfig.GUI_INVERT_TURN_DIRECTIONS;
  if (incidentsCb) incidentsCb.checked = appConfig.GUI_INCIDENTS_ENABLED;
  if (incidentProbRange) {
    incidentProbRange.value = Math.round(appConfig.INCIDENT_PROBABILITY_PER_KM * 100);
  }
  if (incidentProbVal) {
    incidentProbVal.textContent = `${Math.round(appConfig.INCIDENT_PROBABILITY_PER_KM * 100)}%`;
  }
  if (imperialCb) imperialCb.checked = appConfig.GUI_IMPERIAL;
  if (turboAltsRange) turboAltsRange.value = appConfig.TURBO_MAX_ALTERNATIVES;
  if (turboAltsVal) turboAltsVal.textContent = `${appConfig.TURBO_MAX_ALTERNATIVES}`;

  // GUI Toggle
  guiToggle.addEventListener("click", () => {
    const open = gui.classList.toggle("gui-open");
    gui.classList.toggle("gui-collapsed", !open);
    guiToggle.setAttribute("aria-expanded", open ? "true" : "false");
    guiPanel.setAttribute("aria-hidden", open ? "false" : "true");
  });

  // Event Listeners for controls
  if (cruiseRange) {
    cruiseRange.addEventListener("input", (e) => {
      appConfig.GUI_MAX_CRUISE_KMPH = Number(e.target.value);
      cruiseVal.textContent = `${appConfig.GUI_MAX_CRUISE_KMPH} km/h`;
    });
  }

  if (instantAccCb) {
    instantAccCb.addEventListener("change", (e) => {
      appConfig.GUI_INSTANT_ACCEL = e.target.checked;
    });
  }

  if (noSpeedLimitCb) {
    noSpeedLimitCb.addEventListener("change", (e) => {
      appConfig.GUI_NO_SPEED_LIMIT = e.target.checked;
    });
  }

  if (disableSmartCb) {
    disableSmartCb.addEventListener("change", (e) => {
      appConfig.GUI_DISABLE_SMART_SPEED = e.target.checked;
    });
  }

  if (infiniteFuelCb) {
    infiniteFuelCb.addEventListener("change", (e) => {
      appConfig.GUI_INFINITE_FUEL = e.target.checked;
      updateHUD();
    });
  }

  if (invertTurnsCb) {
    invertTurnsCb.addEventListener("change", (e) => {
      appConfig.GUI_INVERT_TURN_DIRECTIONS = e.target.checked;
      updateTurnUI(window._simulationState.routeIndexFloat || 0); // Re-render turn icons
    });
  }

  if (incidentsCb) {
    incidentsCb.addEventListener("change", (e) => {
      appConfig.GUI_INCIDENTS_ENABLED = e.target.checked;
    });
  }
  if (incidentProbRange) {
    incidentProbRange.addEventListener("input", (e) => {
      appConfig.INCIDENT_PROBABILITY_PER_KM = Number(e.target.value) / 100;
      if (incidentProbVal) incidentProbVal.textContent = `${e.target.value}%`;
    });
  }

  if (imperialCb) {
    imperialCb.addEventListener("change", (e) => {
      appConfig.GUI_IMPERIAL = e.target.checked;
      updateHUD();
      updateTurnUI(window._simulationState.routeIndexFloat || 0);
    });
  }

  if (turboAltsRange) {
    turboAltsRange.addEventListener("input", (e) => {
      appConfig.TURBO_MAX_ALTERNATIVES = Number(e.target.value);
      if (turboAltsVal) turboAltsVal.textContent = `${appConfig.TURBO_MAX_ALTERNATIVES}`;
    });
  }

  // Teleport functionality
  if (teleportGo && teleportInput) {
    teleportGo.addEventListener("click", async () => {
      const q = (teleportInput.value || "").trim();
      if (!q) return;
      try {
        showLoading("Searching city...");
        const arr = await searchPlace(q, 1);
        hideLoading();
        if (!arr || arr.length === 0) return;
        const place = arr[0];
        const lat = parseFloat(place.lat);
        const lon = parseFloat(place.lon);

        map.setView([lat, lon], Math.max(12, Math.min(14, map.getZoom())));
        const newPos = L.latLng(lat, lon);
        driverMarker.setLatLng(newPos);
        updateCountryDisplay(newPos);

        if (window._routeLine) {
          const ok = await showConfirmModal();
          if (ok) {
            try { map.removeLayer(window._routeLine); } catch {}
            window._routeLine = null;
            gasMarkers.forEach(m => { try { map.removeLayer(m); } catch { } });
            gasMarkers.length = 0;
            setCurrentRouteName("");
            setCurrentRoadType("");
            updateHUD();
          }
        }
      } catch (e) {
        hideLoading();
        console.warn("Teleport failed", e);
      }
    });
  }

  // Expose tweakables related to GUI controls
  window.__tweakables = window.__tweakables || {};
  Object.assign(window.__tweakables, {
    /* @tweakable [set max cruise km/h] */
    setMaxCruise: (k) => {
      appConfig.GUI_MAX_CRUISE_KMPH = Number(k);
      if (cruiseRange) cruiseRange.value = appConfig.GUI_MAX_CRUISE_KMPH;
      if (cruiseVal) cruiseVal.textContent = `${appConfig.GUI_MAX_CRUISE_KMPH} km/h`;
    },
    /* @tweakable [toggle instant acceleration] */
    setInstantAccel: (b) => {
      appConfig.GUI_INSTANT_ACCEL = Boolean(b);
      if (instantAccCb) instantAccCb.checked = appConfig.GUI_INSTANT_ACCEL;
    },
    /* @tweakable [toggle no speed limit] */
    setNoSpeedLimit: (b) => {
      appConfig.GUI_NO_SPEED_LIMIT = Boolean(b);
      if (noSpeedLimitCb) noSpeedLimitCb.checked = appConfig.GUI_NO_SPEED_LIMIT;
    },
    /* @tweakable [toggle disable smart speed] */
    setDisableSmartSpeed: (b) => {
      appConfig.GUI_DISABLE_SMART_SPEED = Boolean(b);
      if (disableSmartCb) disableSmartCb.checked = appConfig.GUI_DISABLE_SMART_SPEED;
    },
    /* @tweakable [toggle infinite fuel] */
    setInfiniteFuel: (b) => {
      appConfig.GUI_INFINITE_FUEL = Boolean(b);
      if (infiniteFuelCb) infiniteFuelCb.checked = appConfig.GUI_INFINITE_FUEL;
      updateHUD();
    },
    /* @tweakable [toggle imperial units (mph/mi/gal) in HUD and labels] */
    setImperialMode: (b) => {
      appConfig.GUI_IMPERIAL = Boolean(b);
      if (imperialCb) imperialCb.checked = appConfig.GUI_IMPERIAL;
      updateHUD();
    },
    /* @tweakable [toggle invert turn directions] */
    setInvertTurns: (b) => {
      appConfig.GUI_INVERT_TURN_DIRECTIONS = Boolean(b);
      if (invertTurnsCb) invertTurnsCb.checked = appConfig.GUI_INVERT_TURN_DIRECTIONS;
      updateTurnUI(window._simulationState.routeIndexFloat || 0);
    },
    /* @tweakable [toggle enable car incidents] */
    setIncidentsEnabled: (b) => {
      appConfig.GUI_INCIDENTS_ENABLED = Boolean(b);
      if (incidentsCb) incidentsCb.checked = appConfig.GUI_INCIDENTS_ENABLED;
    },
    /* @tweakable [set incident probability per km (0-1, as a percentage)] */
    setIncidentProbabilityPerKm: (p) => {
      appConfig.INCIDENT_PROBABILITY_PER_KM = Number(p);
      if (incidentProbRange) incidentProbRange.value = Math.round(appConfig.INCIDENT_PROBABILITY_PER_KM * 100);
      if (incidentProbVal) incidentProbVal.textContent = `${Math.round(appConfig.INCIDENT_PROBABILITY_PER_KM * 100)}%`;
    },
    /* @tweakable [set max alternatives requested when turbo is on] */
    setTurboMaxAlternatives: (n) => {
      appConfig.TURBO_MAX_ALTERNATIVES = Number(n);
      if (turboAltsRange) turboAltsRange.value = appConfig.TURBO_MAX_ALTERNATIVES;
      if (turboAltsVal) turboAltsVal.textContent = `${appConfig.TURBO_MAX_ALTERNATIVES}`;
    },
    /* @tweakable [toggle turbo mode on/off] */
    setTurboMode: (b) => {
      appConfig.GUI_TURBO_MODE = Boolean(b);
      // GUI currently doesn't have a direct toggle for turbo mode, but it's settable via max alternatives range.
      // If a specific checkbox for turbo mode is added in HTML, it would be wired here.
    },
  });
}