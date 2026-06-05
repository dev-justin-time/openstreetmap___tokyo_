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
  setRouteCoords,
  setRouteOriginalCoords,
  setCurrentCountryName,
  setCurrentRouteName,
  setCurrentRoadType,
} from "./simulation.js";
import { updateHUD, showLoading, hideLoading } from "./ui.js";
import { fetchRoute, fetchRouteAlternatives } from "./api.js";
import { initGUI } from "./gui.js";
import { showConfirmModal } from "./confirmModal.js";
import { initBetaBanner } from "./betaBanner.js";
import { APP_STATE, getDriverIds, registerMarker, removeMarker, setDriversLayerGroup, getDriversLayerGroup, loadRuntimeSettingsFromStorage, persistRuntimeSettingsToStorage } from './app-state.js';
import * as L from "leaflet";

let marker;
let isConnected = false;
let startTime = null;
let connectionHistory = [];
let timerInterval = null;
let watchId = null;

// Expose tweakables for external modification
window.__tweakables = window.__tweakables || {};
Object.assign(window.__tweakables, {
  setBetaBannerText(t) {
    appConfig.OSM_BETA_BANNER_TEXT = String(t);
    const i = document.getElementById("osm-beta-inner");
    if (i) i.textContent = appConfig.OSM_BETA_BANNER_TEXT;
  },
  setBetaBannerVisible(b) {
    appConfig.OSM_BETA_BANNER_VISIBLE = Boolean(b);
    const bEl = document.getElementById("osm-beta-banner");
    if (bEl) {
      bEl.style.display = appConfig.OSM_BETA_BANNER_VISIBLE ? "flex" : "none";
      bEl.setAttribute("aria-hidden", appConfig.OSM_BETA_BANNER_VISIBLE ? "false" : "true");
    }
  },
  setDriverGlow(px) { appConfig.GUI_DRIVER_GLOW_SIZE_PX = px; refreshDriverGlow(); },
  setRouteColor(c) {
    appConfig.GUI_ROUTE_COLOR = c;
    if (window._routeLine) window._routeLine.setStyle({ color: appConfig.GUI_ROUTE_COLOR });
  },
  setRouteWeight(w) {
    appConfig.GUI_ROUTE_WEIGHT_PX = w;
    if (window._routeLine) window._routeLine.setStyle({ weight: appConfig.GUI_ROUTE_WEIGHT_PX });
  },
  setTileRefreshInterval: (ms) => {
    appConfig.TILE_LAYER_FORCED_REFRESH_INTERVAL_MS = Number(ms);
    setupForcedMapRefresh();
  },
  setSpeedMultiplier(m) { appConfig.GUI_SPEED_REALISM_MULTIPLIER = Number(m); },
  setMaxAccel(a) { appConfig.MAX_ACCEL = Number(a); },
  setMaxDecel(d) { appConfig.MAX_DECEL = Number(d); },
  setTurboStrictness(s) { appConfig.TURBO_STRICTNESS = Number(s); },
  setHighwayMinKmph(n) { appConfig.GUI_HIGHWAY_MIN_KMPH = Number(n); },
  setShowCountry(b) { appConfig.GUI_SHOW_COUNTRY = Boolean(b); updateHUD(); },
  setShowInfoPanel(b) { appConfig.GUI_INFO_PANEL_VISIBLE = Boolean(b); import('./ui.js').then(m => m.setInfoPanelVisible(appConfig.GUI_INFO_PANEL_VISIBLE)).catch(() => {}); },
  setCountryGeocodeDebounceMs(ms) { appConfig.COUNTRY_REVERSE_GEOCODE_DEBOUNCE_MS = Number(ms); },
  setCountryGeocodeMinDistM(m) { appConfig.COUNTRY_REVERSE_GEOCODE_MIN_DIST_M = Number(m); }
});

export const GUI_INVERT_TURN_DIRECTIONS = appConfig.GUI_INVERT_TURN_DIRECTIONS;

(async function initApp() {
  try {
    const p = `${appConfig.TOKYO.lat},${appConfig.TOKYO.lng}`;
    const res = await fetch(`${appConfig.OSM_NOMINATIM_BASE}/reverse?format=jsonv2&lat=${appConfig.TOKYO.lat}&lon=${appConfig.TOKYO.lng}`);
    if (res.ok) {
      const j = await res.json();
      const cc = j.address && j.address.country_code ? j.address.country_code.toUpperCase() : null;
      if (cc) setCurrencyForCountryCode(cc);
      if (j.address && j.address.country) {
        setCurrentCountryName(j.address.country);
      }
    }
  } catch (e) {
  } finally {
    updateHUD();
  }

  refreshDriverGlow();
  setupMapRefresh();
  setupForcedMapRefresh();
  initGUI();
  initBetaBanner();
  addMarker(appConfig.TOKYO.lat, appConfig.TOKYO.lng, "Tokyo (approx center)");
  driverMarker.setLatLng([appConfig.TOKYO.lat, appConfig.TOKYO.lng]);
  updateCountryDisplay(driverMarker.getLatLng());
})();

map.on("click", async (e) => {
  try {
    if (window._routeLine) {
      const ok = await showConfirmModal();
      if (!ok) {
        return;
      }
      try { map.removeLayer(window._routeLine); } catch (err) { }
      window._routeLine = null;
      gasMarkers.forEach(m => { try { map.removeLayer(m); } catch { } });
      gasMarkers.length = 0;
    }

    showLoading("Calculating route...");
    const dest = { lat: e.latlng.lat, lng: e.latlng.lng };
    addMarker(dest.lat, dest.lng, `Clicked: ${dest.lat.toFixed(5)}, ${dest.lng.toFixed(5)}`);

    const fromLatLng = driverMarker.getLatLng();

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

    try {
      setCurrentRouteName(route.legs && route.legs.length ? (route.legs[0].summary || "") : (route.summary || ""));
      const anyStepIsHighway = (route.legs || []).some(leg => (leg.steps || []).some(s => {
        const n = (s.name || "").toLowerCase();
        const r = (s.ref || "").toLowerCase();
        const motorwayTokens = ["motorway", "高速", "expressway", "highway", "autobahn", "shuto", "route", "i-", "route"];
        return motorwayTokens.some(t => n.includes(t) || r.includes(t));
      }));
      setCurrentRoadType(anyStepIsHighway ? "highway" : "road");
    } catch (e) {
      setCurrentRouteName("");
      setCurrentRoadType("");
    }

    window._routeDurationSec = route.duration != null ? route.duration : null;

    hideLoading();

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
    } catch (e) { console.warn("Failed to set route data attribute", e); }

    setRouteOriginalCoords(coords.slice());
    setRouteCoords(coords.slice());
    placeGasStations(dest);
    startAnimation(route.geometry, route.legs.flatMap(leg => leg.steps));

  } catch (err) {
    hideLoading();
    console.warn("Route error:", err);
  }
});

function initMap() {
  const layer = L.layerGroup().addTo(map);
  setDriversLayerGroup(layer);
  setupLocationTracking();
  startDriversPolling();
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
    btn.textContent = appConfig.UI_LABELS.disconnect;
    btn.classList.add('connected');
    startTime = Date.now();
    timerContainer.classList.remove('hidden');
    updateTimer();
    timerInterval = setInterval(updateTimer, 1000);
    startWatchingPosition();
  } else {
    btn.textContent = appConfig.UI_LABELS.connect;
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

async function fetchAndRenderDrivers() {
  try {
    const resp = await fetch('http://127.0.0.1:3030/drivers');
    if (!resp.ok) return;
    const json = await resp.json();
    if (!json || !json.drivers) return;

    const currentIds = new Set(json.drivers.map(d => d.id));
    const existingIds = getDriverIds();
    existingIds.forEach(id => {
      if (!currentIds.has(id)) {
        removeMarker(id);
      }
    });

    const layer = getDriversLayerGroup();
    json.drivers.forEach(d => {
      const id = d.id;
      const lat = d.lat;
      const lon = d.lon;
      const status = (d.status || '').toLowerCase();
      const color = status === 'available' ? '#2ecc71' : '#ff375f';

      const existing = APP_STATE.driverMarkers[id];
      if (!existing) {
        const icon = L.divIcon({
          className: 'driver-marker',
          html: `<div style="width:14px;height:14px;border-radius:50%;background:${color};border:2px solid white;"></div>`,
          iconSize: [18, 18],
          iconAnchor: [9, 9]
        });
        const m = L.marker([lat, lon], { icon }).bindTooltip(id, { direction: 'top', offset: [0, -10] });
        if (layer) m.addTo(layer);
        registerMarker(id, m);
      } else {
        existing.setLatLng([lat, lon]);
        const icon = L.divIcon({
          className: 'driver-marker',
          html: `<div style="width:14px;height:14px;border-radius:50%;background:${color};border:2px solid white;"></div>`,
          iconSize: [18, 18],
          iconAnchor: [9, 9]
        });
        existing.setIcon(icon);
      }
    });
  } catch (e) {
  }
}

let driversPollingInterval = null;
function startDriversPolling() {
  const settings = loadRuntimeSettingsFromStorage();
  const ms = settings && settings.pollInterval ? parseInt(settings.pollInterval, 10) : 2000;
  fetchAndRenderDrivers();
  driversPollingInterval = setInterval(fetchAndRenderDrivers, ms);
}
function stopDriversPolling() {
  if (driversPollingInterval) {
    clearInterval(driversPollingInterval);
    driversPollingInterval = null;
  }
}

function loadRuntimeSettings() {
  const pollIntervalInput = document.getElementById('poll-interval');
  const simBatchInput = document.getElementById('sim-batch-size');
  const rustAddrsInput = document.getElementById('rust-addrs');
  const pollToggleBtn = document.getElementById('poll-toggle');

  const settings = JSON.parse(localStorage.getItem('runtimeSettings') || '{}');
  pollIntervalInput.value = settings.pollInterval || 2000;
  simBatchInput.value = settings.simBatchSize || 50;
  rustAddrsInput.value = settings.rustAddrs || 'http://127.0.0.1:3030';
  pollToggleBtn.textContent = driversPollingInterval ? 'Stop polling' : 'Start polling';

  if (driversPollingInterval) {
    clearInterval(driversPollingInterval);
    driversPollingInterval = setInterval(fetchAndRenderDrivers, parseInt(pollIntervalInput.value, 10));
  }
}

function toggleDriversPolling() {
  const pollBtn = document.getElementById('poll-toggle');
  const pollIntervalInput = document.getElementById('poll-interval');
  if (!pollBtn || !pollIntervalInput) return;
  if (driversPollingInterval) {
    stopDriversPolling();
    pollBtn.textContent = appConfig.UI_LABELS.startPolling;
  } else {
    const ms = Math.max(500, parseInt(pollIntervalInput.value || '2000', 10));
    driversPollingInterval = setInterval(fetchAndRenderDrivers, ms);
    fetchAndRenderDrivers();
    pollBtn.textContent = appConfig.UI_LABELS.stopPolling;
  }
  persistRuntimeSettingsToStorage({
    pollInterval: parseInt(document.getElementById('poll-interval').value || '2000', 10),
    simBatchSize: parseInt(document.getElementById('sim-batch-size').value || '50', 10),
    rustAddrs: (document.getElementById('rust-addrs').value || 'http://127.0.0.1:3030').trim()
  });
}

function persistRuntimeSettings() {
  const pollIntervalInput = document.getElementById('poll-interval');
  const simBatchInput = document.getElementById('sim-batch-size');
  const rustAddrsInput = document.getElementById('rust-addrs');
  const settings = {
    pollInterval: parseInt(pollIntervalInput.value || '2000', 10),
    simBatchSize: parseInt(simBatchInput.value || '50', 10),
    rustAddrs: (rustAddrsInput.value || 'http://127.0.0.1:3030').trim()
  };
  localStorage.setItem('runtimeSettings', JSON.stringify(settings));
  if (driversPollingInterval) {
    clearInterval(driversPollingInterval);
    driversPollingInterval = setInterval(fetchAndRenderDrivers, settings.pollInterval);
  }
}

function openGoMetrics() {
  window.open('/metrics', '_blank');
}
function openRustMetrics() {
  const settings = JSON.parse(localStorage.getItem('runtimeSettings') || '{}');
  const addrs = (settings.rustAddrs || 'http://127.0.0.1:3030').split(',').map(s => s.trim()).filter(Boolean);
  window.open((addrs[0] || 'http://127.0.0.1:3030') + '/metrics', '_blank');
}

let simRunning = false;
let simIntervalHandle = null;

function startSimulatorFromUI() {
  persistRuntimeSettingsToStorage({
    pollInterval: parseInt(document.getElementById('poll-interval').value || '2000', 10),
    simBatchSize: parseInt(document.getElementById('sim-batch-size').value || '50', 10),
    rustAddrs: (document.getElementById('rust-addrs').value || 'http://127.0.0.1:3030').trim()
  });
  const count = parseInt(document.getElementById('sim-count').value || '100', 10);
  const settings = JSON.parse(localStorage.getItem('runtimeSettings') || '{}');
  const rustAddrs = settings.rustAddrs || 'http://127.0.0.1:3030';
  fetch('/generate-drivers?count=' + encodeURIComponent(Math.max(1, count))).then(r => {
    if (r.ok) {
      document.getElementById('gear-output').textContent = `Generator started: count=${count}`;
    } else {
      document.getElementById('gear-output').textContent = `Generator unavailable (HTTP ${r.status})`;
    }
  }).catch(e => {
    document.getElementById('gear-output').textContent = 'Error contacting generator: ' + e.message;
  });
}

function stopSimulatorFromUI() {
  document.getElementById('gear-output').textContent = 'Stop simulator request sent (if available).';
}

function toggleGear() {
  const panel = document.getElementById('gear-panel');
  if (!panel) return;
  const isHidden = panel.getAttribute('aria-hidden') === 'true';
  panel.setAttribute('aria-hidden', isHidden ? 'false' : 'true');

  if (isHidden) {
    populateDriverSelect();
    switchGearTab('track');
    loadRuntimeSettings();
  }
}

function switchGearTab(tab) {
  const title = document.getElementById('gear-title');
  const tabTrackBtn = document.getElementById('tab-track');
  const tabAssignBtn = document.getElementById('tab-assign');
  const tabRuntimeBtn = document.getElementById('tab-runtime');
  const trackPanel = document.getElementById('gear-tab-track');
  const assignPanel = document.getElementById('gear-tab-assign');
  const runtimePanel = document.getElementById('gear-tab-runtime');

  tabTrackBtn.setAttribute('aria-selected', 'false');
  tabAssignBtn.setAttribute('aria-selected', 'false');
  tabRuntimeBtn.setAttribute('aria-selected', 'false');
  trackPanel.classList.add('hidden');
  assignPanel.classList.add('hidden');
  runtimePanel.classList.add('hidden');
  assignPanel.setAttribute('aria-hidden', 'true');
  runtimePanel.setAttribute('aria-hidden', 'true');

  if (tab === 'assign') {
    title.textContent = appConfig.UI_LABELS.assignments;
    tabAssignBtn.setAttribute('aria-selected', 'true');
    assignPanel.classList.remove('hidden');
    assignPanel.setAttribute('aria-hidden', 'false');
  } else if (tab === 'runtime') {
    title.textContent = appConfig.UI_LABELS.runtime;
    tabRuntimeBtn.setAttribute('aria-selected', 'true');
    runtimePanel.classList.remove('hidden');
    runtimePanel.setAttribute('aria-hidden', 'false');
  } else {
    title.textContent = appConfig.UI_LABELS.whatToTrack;
    tabTrackBtn.setAttribute('aria-selected', 'true');
    trackPanel.classList.remove('hidden');
  }
}

function populateDriverSelect() {
  const sel = document.getElementById('assign-driver-select');
  if (!sel) return;
  sel.innerHTML = '';
  const ids = getDriverIds();
  if (ids.length === 0) {
    const opt = document.createElement('option');
    opt.value = '';
    opt.textContent = appConfig.UI_LABELS.noDrivers;
    sel.appendChild(opt);
    return;
  }
  const blank = document.createElement('option');
  blank.value = '';
  blank.textContent = appConfig.UI_LABELS.selectDriver;
  sel.appendChild(blank);
  ids.forEach(id => {
    const opt = document.createElement('option');
    opt.value = id;
    opt.textContent = id;
    sel.appendChild(opt);
  });
}

function clearAssignmentForm() {
  const fields = ['assign-driver-select','assign-type','assign-priority','assign-start','assign-end','assign-notes'];
  fields.forEach(id => {
    const el = document.getElementById(id);
    if (!el) return;
    if (el.tagName === 'SELECT') el.selectedIndex = 0;
    else if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') el.value = '';
  });
  const out = document.getElementById('gear-output');
  if (out) out.textContent = '';
}

function assignToDriver(evt) {
  if (evt && evt.preventDefault) evt.preventDefault();
  const sel = document.getElementById('assign-driver-select');
  const type = document.getElementById('assign-type');
  const prio = document.getElementById('assign-priority');
  const start = document.getElementById('assign-start');
  const end = document.getElementById('assign-end');
  const notes = document.getElementById('assign-notes');
  const out = document.getElementById('gear-output');
  if (!sel || !out) return;
  const driverId = sel.value;
  if (!driverId) {
    out.textContent = appConfig.UI_LABELS.selectDriverToAssign;
    return;
  }
  const payload = {
    driver_id: driverId,
    type: type ? type.value : 'pickup',
    priority: prio ? prio.value : 'normal',
    start: start && start.value ? start.value : null,
    end: end && end.value ? end.value : null,
    notes: notes ? notes.value : null,
    created_at: new Date().toISOString()
  };

  out.textContent = JSON.stringify(payload, null, 2);

  try {
    const m = APP_STATE.driverMarkers[driverId];
    if (m) {
      m.openTooltip();
      setTimeout(() => { try { m.closeTooltip(); } catch(e){} }, 2500);
    }
  } catch (e) {}
}

async function returnTrackedData() {
  const out = document.getElementById('gear-output');
  if (!out) return;
  const form = document.getElementById('track-form');
  const formData = new FormData(form);
  const selection = {
    location: formData.get('trackLocation') === 'on' || formData.has('trackLocation'),
    speed: formData.get('trackSpeed') === 'on' || formData.has('trackSpeed'),
    heading: formData.get('trackHeading') === 'on' || formData.has('trackHeading'),
    status: formData.get('trackStatus') === 'on' || formData.has('trackStatus'),
  };

  const data = {};
  try {
    if (selection.location && marker) {
      const latlng = marker.getLatLng();
      data.location = { lat: latlng.lat, lon: latlng.lng };
    }
    if (selection.speed || selection.heading) {
      const pos = await new Promise((res, rej) => {
        if (!navigator.geolocation) return res(null);
        navigator.geolocation.getCurrentPosition(p => res(p), () => res(null), {enableHighAccuracy:true, timeout:3000});
      });
      if (pos) {
        if (selection.speed) data.speed_m_s = pos.coords.speed === null ? null : pos.coords.speed;
        if (selection.heading) data.heading_deg = pos.coords.heading === null ? null : pos.coords.heading;
      }
    }
    if (selection.status) data.connected = !!isConnected;
    data.timestamp = Date.now();
  } catch (e) {
    data.error = String(e);
  }

  out.textContent = JSON.stringify(data, null, 2);
  const panel = document.getElementById('gear-panel');
  if (panel) panel.setAttribute('aria-hidden', 'false');
}

window.onload = async () => {
  initMap();
  loadHistory();

  let uploadGpxFile = null;
  try {
    const mod = await import('./api-client.js');
    uploadGpxFile = mod.uploadGpxFile;
  } catch (e) {
    console.warn('Failed to load api-client dynamically:', e);
  }

  const fileInput = document.getElementById('gpx-file');
  const resultDiv = document.getElementById('gpx-result');
  if (fileInput) {
    fileInput.addEventListener('change', async (e) => {
      const f = e.target.files[0];
      if (!f) return;
      resultDiv.textContent = 'Uploading and processing GPX\u2026';
      if (!uploadGpxFile) {
        resultDiv.textContent = 'Upload unavailable (client module failed to load)';
        return;
      }
      try {
        const data = await uploadGpxFile(f);
        try {
          const pretty = JSON.stringify(data, null, 2);
          resultDiv.textContent = pretty;
        } catch (err) {
          resultDiv.textContent = 'Processed (see console)';
          console.log('GPX response:', data);
        }
      } catch (err) {
        resultDiv.textContent = 'Upload failed: ' + err.message;
      }
    });
  }
};

window.toggleProfile = toggleProfile;
window.toggleConnection = toggleConnection;
window.toggleGear = toggleGear;
window.returnTrackedData = returnTrackedData;
window.switchGearTab = switchGearTab;
window.populateDriverSelect = populateDriverSelect;
window.assignToDriver = assignToDriver;
window.clearAssignmentForm = clearAssignmentForm;
window.toggleDriversPolling = toggleDriversPolling;
window.loadRuntimeSettings = loadRuntimeSettings;
window.persistRuntimeSettings = persistRuntimeSettings;
window.startSimulatorFromUI = startSimulatorFromUI;
window.stopSimulatorFromUI = stopSimulatorFromUI;
window.openGoMetrics = openGoMetrics;
window.openRustMetrics = openRustMetrics;
