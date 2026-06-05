let map;
let marker;
let isConnected = false;
let startTime = null;
let connectionHistory = [];
let timerInterval = null;
let watchId = null;









/* ...existing code... */
import { appConfig } from "./config.js";
import { map, driverMarker, addMarker, refreshDriverGlow, setupMapRefresh, setupForcedMapRefresh } from "./map.js";
import {
  startAnimation,
  setCurrencyForCountryCode,
  updateCountryDisplay,
  CURRENT_ROUTE_NAME,
  CURRENT_ROAD_TYPE,
  placeGasStations,
  gasMarkers,
  CURRENT_COUNTRY_NAME,
  setFollowCar,
} from "./simulation.js";
import { updateHUD, showLoading, hideLoading } from "./ui.js";
import { fetchRoute, fetchRouteAlternatives } from "./api.js";
import { initGUI } from "./gui.js";
import { showConfirmModal } from "./confirmModal.js";
import { initBetaBanner } from "./betaBanner.js";

// Expose tweakables for external modification
window.__tweakables = window.__tweakables || {};
Object.assign(window.__tweakables, {
  /* @tweakable [set the beta banner text shown to users] */
  setBetaBannerText(t) {
    appConfig.OSM_BETA_BANNER_TEXT = String(t);
    const i = document.getElementById("osm-beta-inner");
    if (i) i.textContent = appConfig.OSM_BETA_BANNER_TEXT;
  },
  /* @tweakable [toggle beta banner visibility] */
  setBetaBannerVisible(b) {
    appConfig.OSM_BETA_BANNER_VISIBLE = Boolean(b);
    const bEl = document.getElementById("osm-beta-banner");
    if (bEl) {
      bEl.style.display = appConfig.OSM_BETA_BANNER_VISIBLE ? "flex" : "none";
      bEl.setAttribute("aria-hidden", appConfig.OSM_BETA_BANNER_VISIBLE ? "false" : "true");
    }
  },
  // Map specific tweaks
  /* @tweakable [Set the driver marker glow size in pixels] */
  setDriverGlow(px) { appConfig.GUI_DRIVER_GLOW_SIZE_PX = px; refreshDriverGlow(); },
  /* @tweakable [Set the route line color] */
  setRouteColor(c) {
    appConfig.GUI_ROUTE_COLOR = c;
    if (window._routeLine) window._routeLine.setStyle({ color: appConfig.GUI_ROUTE_COLOR });
  },
  /* @tweakable [Set the route line weight (thickness) in pixels] */
  setRouteWeight(w) {
    appConfig.GUI_ROUTE_WEIGHT_PX = w;
    if (window._routeLine) window._routeLine.setStyle({ weight: appConfig.GUI_ROUTE_WEIGHT_PX });
  },
  /* @tweakable [Set the interval in milliseconds for forced periodic tile layer refreshing (0 to disable)] */
  setTileRefreshInterval: (ms) => {
    appConfig.TILE_LAYER_FORCED_REFRESH_INTERVAL_MS = Number(ms);
    setupForcedMapRefresh(); // Re-initialize the interval with the new setting
  },
  // Simulation specific tweaks
  /* @tweakable [global speed realism multiplier; >1 = faster/less conservative speeds] */
  setSpeedMultiplier(m) { appConfig.GUI_SPEED_REALISM_MULTIPLIER = Number(m); },
  /* @tweakable [set max acceleration (m/s^2)] */
  setMaxAccel(a) { appConfig.MAX_ACCEL = Number(a); },
  /* @tweakable [set max deceleration (m/s^2)] */
  setMaxDecel(d) { appConfig.MAX_DECEL = Number(d); },
  /* @tweakable [turbo strictness (0-1) biasing duration vs distance when selecting best alternative] */
  setTurboStrictness(s) { appConfig.TURBO_STRICTNESS = Number(s); },
  /* @tweakable [minimum enforced highway speed in km/h] */
  setHighwayMinKmph(n) { appConfig.GUI_HIGHWAY_MIN_KMPH = Number(n); },
  /* @tweakable [toggle showing the current country name in the HUD] */
  setShowCountry(b) { appConfig.GUI_SHOW_COUNTRY = Boolean(b); updateHUD(); },
  /* @tweakable [toggle visibility of the HUD/info panel (true = visible)] */
  setShowInfoPanel(b) { appConfig.GUI_INFO_PANEL_VISIBLE = Boolean(b); import('./ui.js').then(m => m.setInfoPanelVisible(appConfig.GUI_INFO_PANEL_VISIBLE)).catch(() => {}); },
  /* @tweakable [debounce time in ms for reverse geocoding calls to update country display] */
  setCountryGeocodeDebounceMs(ms) { appConfig.COUNTRY_REVERSE_GEOCODE_DEBOUNCE_MS = Number(ms); },
  /* @tweakable [minimum distance in meters the car must move before re-fetching country name] */
  setCountryGeocodeMinDistM(m) { appConfig.COUNTRY_REVERSE_GEOCODE_MIN_DIST_M = Number(m); }
});

// expose the current invert-turns flag as a named export to avoid import errors from code expecting it
/* @tweakable [export proxy for invert turn directions flag so legacy imports from ./app.js work] */
export const GUI_INVERT_TURN_DIRECTIONS = appConfig.GUI_INVERT_TURN_DIRECTIONS;

// Initial setup functions
(async function initApp() {
  // Try to reverse geocode driver start to set currency and country name
  try {
    const p = `${appConfig.TOKYO.lat},${appConfig.TOKYO.lng}`;
    const res = await fetch(`https://nominatim.openstreetmap.org/reverse?format=jsonv2&lat=${appConfig.TOKYO.lat}&lon=${appConfig.TOKYO.lng}`);
    if (res.ok) {
      const j = await res.json();
      const cc = j.address && j.address.country_code ? j.address.country_code.toUpperCase() : null;
      if (cc) setCurrencyForCountryCode(cc);
      if (j.address && j.address.country) {
        /* @tweakable [use setter to avoid assigning to read-only module export] */
        import('./simulation.js').then(sim => sim.setCurrentCountryName(j.address.country));
      }
    }
  } catch (e) {
    // ignore; keep defaults
  } finally {
    updateHUD(); // Ensure HUD reflects initial country/currency
  }

  // Set initial driver glow
  refreshDriverGlow();

  // Setup map refresh on interactions
  setupMapRefresh();
  // Setup periodic forced map refresh
  setupForcedMapRefresh();

  // Initialize GUI controls and their listeners
  initGUI();

  // Initialize beta banner
  initBetaBanner();

  // Add a subtle initial marker for the city center
  addMarker(appConfig.TOKYO.lat, appConfig.TOKYO.lng, "Tokyo (approx center)");
  // ensure driver circle placed at start
  driverMarker.setLatLng([appConfig.TOKYO.lat, appConfig.TOKYO.lng]);
  updateCountryDisplay(driverMarker.getLatLng()); // Initial country display update

})();

// Click to add marker: compute route from driver current position to clicked point and animate
map.on("click", async (e) => {
  try {
    // If there's an existing route line, confirm replacement
    if (window._routeLine) {
      const ok = await showConfirmModal();
      if (!ok) {
        // user cancelled; do nothing
        return;
      }
      // user confirmed: remove existing route & gas markers to prepare for new route
      try { map.removeLayer(window._routeLine); } catch (err) { /* ignore */ }
      window._routeLine = null;
      gasMarkers.forEach(m => { try { map.removeLayer(m); } catch { } });
      gasMarkers.length = 0;
    }

    showLoading("Calculating route...");
    const dest = { lat: e.latlng.lat, lng: e.latlng.lng };
    addMarker(dest.lat, dest.lng, `Clicked: ${dest.lat.toFixed(5)}, ${dest.lng.toFixed(5)}`);

    // Build route from driver's current location (driverMarker) to dest
    const fromLatLng = driverMarker.getLatLng();

    // If turbo is enabled, request multiple alternatives and pick the fastest
    let route;
    if (appConfig.GUI_TURBO_MODE) {
      try {
        route = await fetchRouteAlternatives({ lat: fromLatLng.lat, lng: fromLatLng.lng }, dest, appConfig.TURBO_MAX_ALTERNATIVES);
      } catch (altErr) {
        console.warn("Turbo alternatives failed, falling back to single route", altErr);
        route = await fetchRoute({ lat: fromLatLng.lat, lng: fromLatLng.lng }, dest);
      }
    } else {
      route = await fetchRoute({ lat: fromLatLng.lat, lng: fromLatLng.lng }, dest);
    }

    // record route name/summary
    try {
      /* @tweakable [use setters instead of assigning to exported module properties (prevents read-only errors)] */
      import('./simulation.js').then(sim => {
        sim.setCurrentRouteName(route.legs && route.legs.length ? (route.legs[0].summary || "") : (route.summary || ""));
        const anyStepIsHighway = (route.legs || []).some(leg => (leg.steps || []).some(s => {
          const n = (s.name || "").toLowerCase();
          const r = (s.ref || "").toLowerCase();
          const motorwayTokens = ["motorway", "高速", "expressway", "highway", "autobahn", "shuto", "route", "i-", "route"];
          return motorwayTokens.some(t => n.includes(t) || r.includes(t));
        }));
        sim.setCurrentRoadType(anyStepIsHighway ? "highway" : "road");
      });
    } catch (e) {
      import('./simulation.js').then(sim => {
        sim.setCurrentRouteName("");
        sim.setCurrentRoadType("");
      });
    }

    // store duration for ETA fallback (seconds)
    window._routeDurationSec = route.duration != null ? route.duration : null;

    hideLoading();

    // Draw route polyline
    if (window._routeLine) {
      map.removeLayer(window._routeLine);
    }
    const coords = route.geometry.coordinates.map(c => [c[1], c[0]]);
    window._routeLine = L.polyline(coords, {
      color: appConfig.GUI_ROUTE_COLOR,
      weight: appConfig.GUI_ROUTE_WEIGHT_PX,
      opacity: 0.95,
      lineJoin: 'round',
      lineCap: 'round'
    }).addTo(map);
    try {
      const el = window._routeLine && window._routeLine._path;
      if (el) el.setAttribute("data-route", "true");
    } catch (e) { }

    import('./simulation.js').then(sim => {
      sim.routeOriginalCoords = coords.slice();
      sim.routeCoords = coords.slice();
      sim.placeGasStations(dest);
      // Start animation using route geometry and steps
      sim.startAnimation(route.geometry, route.legs.flatMap(leg => leg.steps));
    });

  } catch (err) {
    hideLoading();
    console.warn("Route error:", err);
  }
});
function initMap() {
  map = L.map('map').setView([19.4326, -99.1332], 15);

  L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: ' OpenStreetMap contributors'
  }).addTo(map);

  setupLocationTracking();
}

function setupLocationTracking() {
  if (!navigator.geolocation) {
      showError('Geolocation not available in your browser');
    return;
  }

  navigator.geolocation.getCurrentPosition(
    position => {
      const pos = {
        lat: position.coords.latitude,
        lng: position.coords.longitude
      };

      map.setView([pos.lat, pos.lng], 15);

      if (!marker) {
        const customIcon = L.divIcon({
          className: 'custom-marker',
          html: '<div class="marker-inner"></div>',
          iconSize: [20, 20]
        });

        marker = L.marker([pos.lat, pos.lng], {
          icon: customIcon
        }).addTo(map);
      } else {
        marker.setLatLng([pos.lat, pos.lng]);
      }
    },
    error => {
      showError('Error getting location: ' + error.message);
    },
    {
      enableHighAccuracy: true,
      timeout: 5000,
      maximumAge: 0
    }
  );
}

function showError(message) {
  const mapDiv = document.getElementById('map');
  const existing = mapDiv.querySelector('.map-error');
  if (existing) existing.remove();
  const errorDiv = document.createElement('div');
  errorDiv.className = 'map-error';
  errorDiv.textContent = message;
  mapDiv.appendChild(errorDiv);
}

function updateTimer() {
  if (!startTime) return;
  const elapsedTime = Date.now() - startTime;
  const minutes = Math.floor(elapsedTime / 60000);
  const seconds = Math.floor((elapsedTime % 60000) / 1000);

  const timerDisplay = document.getElementById('timer-display');
  timerDisplay.textContent = `${minutes.toString().padStart(2, '0')}:${seconds.toString().padStart(2, '0')}`;
}

function startWatchingPosition() {
  if (!navigator.geolocation) return;
  if (watchId !== null) return;
  watchId = navigator.geolocation.watchPosition(
    (position) => {
      const pos = {
        lat: position.coords.latitude,
        lng: position.coords.longitude
      };
      if (marker) marker.setLatLng([pos.lat, pos.lng]);
    },
    error => {
      showError('Error following location: ' + error.message);
    },
    {
      enableHighAccuracy: true,
      timeout: 5000,
      maximumAge: 0
    }
  );
}

function stopWatchingPosition() {
  if (watchId !== null && navigator.geolocation) {
    navigator.geolocation.clearWatch(watchId);
    watchId = null;
  }
}

function saveHistory() {
  try {
    const serialized = JSON.stringify(connectionHistory);
    localStorage.setItem('connectionHistory', serialized);
  } catch (e) {
    console.warn('Failed to save history', e);
  }
}

function loadHistory() {
  try {
    const raw = localStorage.getItem('connectionHistory');
    if (!raw) return;
    const parsed = JSON.parse(raw);
    // migrate older Date objects if any
    connectionHistory = parsed.map(item => ({
      date: new Date(item.date),
      duration: item.duration
    }));
    updateConnectionHistory();
  } catch (e) {
    console.warn('Failed to load history', e);
  }
}

function toggleConnection() {
  const btn = document.getElementById('connect-btn');
  const timerContainer = document.getElementById('timer-container');

  isConnected = !isConnected;

  if (isConnected) {
    btn.textContent = 'Disconnect';
    btn.classList.add('connected');
    startTime = Date.now();
    timerContainer.classList.remove('hidden');
    updateTimer();
    timerInterval = setInterval(updateTimer, 1000);
    startWatchingPosition();
  } else {
    btn.textContent = 'Connect';
    btn.classList.remove('connected');
    timerContainer.classList.add('hidden');
    clearInterval(timerInterval);
    stopWatchingPosition();

    const endTime = Date.now();
    const duration = endTime - startTime;
    connectionHistory.unshift({
      date: new Date(startTime),
      duration: duration
    });
    saveHistory();
    updateConnectionHistory();
    startTime = null;
  }
}

function toggleProfile() {
  const panel = document.getElementById('profile-panel');
  panel.classList.toggle('visible');
}

function updateConnectionHistory() {
  const list = document.getElementById('connection-list');
  list.innerHTML = '';

  connectionHistory.forEach(conn => {
    const entry = document.createElement('div');
    entry.className = 'connection-entry';
    const durationMin = Math.floor(conn.duration / 60000);
    const durationSec = Math.floor((conn.duration % 60000) / 1000);
    const pad = (n) => n.toString().padStart(2, '0');
    entry.textContent = `${conn.date.toLocaleDateString()} - ${conn.date.toLocaleTimeString()} (${durationMin}m ${pad(durationSec)}s)`;
    list.appendChild(entry);
  });
}

window.onload = () => {
  initMap();
  loadHistory();

  // small hook to demo uploading a GPX file if an <input id="gpx-file"> exists
  const fileInput = document.getElementById('gpx-file');
  if (fileInput) {
    fileInput.addEventListener('change', async (e) => {
      const f = e.target.files[0];
      if (!f) return;
      try {
        const fd = new FormData();
        fd.append('gpx', f);
        const res = await fetch('/upload', { method: 'POST', body: fd });
        const data = await res.json();
        alert('GPX processed: points=' + (data.points || 0));
      } catch (err) {
        alert('Upload failed: ' + err.message);
      }
    });
  }
};

// Expose functions used by inline onclick attributes when this file is loaded as a module
window.toggleProfile = toggleProfile;
window.toggleConnection = toggleConnection;