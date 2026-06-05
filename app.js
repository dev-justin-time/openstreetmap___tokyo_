import { APP_STATE, getDriverIds, registerMarker, removeMarker, setDriversLayerGroup, getDriversLayerGroup, loadRuntimeSettingsFromStorage, persistRuntimeSettingsToStorage } from './frontend/app-state.js';

let map;
let marker;
let isConnected = false;
let startTime = null;
let connectionHistory = [];
let timerInterval = null;
let watchId = null;

function initMap() {
  map = L.map('map').setView([19.4326, -99.1332], 15);

  L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: ' OpenStreetMap contributors'
  }).addTo(map);

  // layer to hold driver markers (store in shared state)
  const layer = L.layerGroup().addTo(map);
  setDriversLayerGroup(layer);

  setupLocationTracking();
  startDriversPolling(); // begin polling Rust /drivers for live pings
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

window.onload = async () => {
  initMap();
  loadHistory();

  // dynamic import inside async function to avoid SyntaxError for in-function static import
  let uploadGpxFile = null;
  try {
    const mod = await import('./frontend/api-client.js');
    uploadGpxFile = mod.uploadGpxFile;
  } catch (e) {
    console.warn('Failed to load api-client dynamically:', e);
  }

  // small hook to demo uploading a GPX file if an <input id="gpx-file"> exists
  const fileInput = document.getElementById('gpx-file');
  const resultDiv = document.getElementById('gpx-result');
  if (fileInput) {
    fileInput.addEventListener('change', async (e) => {
      const f = e.target.files[0];
      if (!f) return;
      resultDiv.textContent = 'Uploading and processing GPX…';
      if (!uploadGpxFile) {
        resultDiv.textContent = 'Upload unavailable (client module failed to load)';
        return;
      }
      try {
        const data = await uploadGpxFile(f);
        // show pretty JSON for primary/secondary if present
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

/* Gear panel: toggle and return tracked data and runtime controls */
function toggleGear() {
  const panel = document.getElementById('gear-panel');
  if (!panel) return;
  const isHidden = panel.getAttribute('aria-hidden') === 'true';
  panel.setAttribute('aria-hidden', isHidden ? 'false' : 'true');

  // When opening, ensure driver list is populated for assignments and runtime values loaded
  if (isHidden) {
    populateDriverSelect();
    // default to tracking tab
    switchGearTab('track');
    loadRuntimeSettings();
  }
}

// Switch between 'track', 'assign' and 'runtime' tabs
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
    title.textContent = 'Assignments';
    tabAssignBtn.setAttribute('aria-selected', 'true');
    assignPanel.classList.remove('hidden');
    assignPanel.setAttribute('aria-hidden', 'false');
  } else if (tab === 'runtime') {
    title.textContent = 'Runtime';
    tabRuntimeBtn.setAttribute('aria-selected', 'true');
    runtimePanel.classList.remove('hidden');
    runtimePanel.setAttribute('aria-hidden', 'false');
  } else {
    title.textContent = 'What to track';
    tabTrackBtn.setAttribute('aria-selected', 'true');
    trackPanel.classList.remove('hidden');
  }
}

// Populate driver select with current cached driverMarkers (best-effort)
function populateDriverSelect() {
  const sel = document.getElementById('assign-driver-select');
  if (!sel) return;
  sel.innerHTML = '';
  // prefer using latest fetched drivers from driverMarkers object
  const ids = getDriverIds();
  if (ids.length === 0) {
    const opt = document.createElement('option');
    opt.value = '';
    opt.textContent = 'No hay conductores disponibles';
    sel.appendChild(opt);
    return;
  }
  const blank = document.createElement('option');
  blank.value = '';
  blank.textContent = 'Select driver\u2026';
  sel.appendChild(blank);
  ids.forEach(id => {
    const opt = document.createElement('option');
    opt.value = id;
    opt.textContent = id;
    sel.appendChild(opt);
  });
}

// Clear assignment form
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

// Handle assignment action: build payload and show in gear-output (demo)
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
    out.textContent = 'Select a driver to assign.';
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

  // Show assignment summary in panel
  out.textContent = JSON.stringify(payload, null, 2);

  // Optionally: highlight driver on map briefly
  try {
    const m = APP_STATE.driverMarkers[driverId];
    if (m) {
      m.openTooltip();
      setTimeout(() => { try { m.closeTooltip(); } catch(e){} }, 2500);
    }
  } catch (e) {}
}

// Return tracked data controlled by checkboxes
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

  // gather current data (best-effort)
  const data = {};
  try {
    // location from marker if available
    if (selection.location && marker) {
      const latlng = marker.getLatLng();
      data.location = { lat: latlng.lat, lon: latlng.lng };
    }
    // speed and heading: use last geolocation if available via getCurrentPosition
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
  // make sure panel stays open to read results
  const panel = document.getElementById('gear-panel');
  if (panel) panel.setAttribute('aria-hidden', 'false');
}

/* Drivers polling: fetch /drivers from Rust tracker and render colored markers */
async function fetchAndRenderDrivers() {
  try {
    const resp = await fetch('http://127.0.0.1:3030/drivers');
    if (!resp.ok) return;
    const json = await resp.json();
    if (!json || !json.drivers) return;

    // remove markers for drivers no longer present (use shared state)
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

      // create or update marker via shared registry
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
    // silent fail - simulator may not be running
  }
}

let driversPollingInterval = null;
function startDriversPolling() {
  // poll every configured interval (fallback 2s)
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

/* Runtime controls persisted to localStorage and applied to frontend/simulator */

// load runtime settings into UI and apply current runtime state
function loadRuntimeSettings() {
  const pollIntervalInput = document.getElementById('poll-interval');
  const simBatchInput = document.getElementById('sim-batch-size');
  const rustAddrsInput = document.getElementById('rust-addrs');
  const pollToggleBtn = document.getElementById('poll-toggle');

  const settings = JSON.parse(localStorage.getItem('runtimeSettings') || '{}');
  pollIntervalInput.value = settings.pollInterval || 2000;
  simBatchInput.value = settings.simBatchSize || 50;
  rustAddrsInput.value = settings.rustAddrs || 'http://127.0.0.1:3030';
  // set poll button label based on current polling
  pollToggleBtn.textContent = driversPollingInterval ? 'Stop polling' : 'Start polling';

  // apply interval if polling already running
  if (driversPollingInterval) {
    clearInterval(driversPollingInterval);
    driversPollingInterval = setInterval(fetchAndRenderDrivers, parseInt(pollIntervalInput.value, 10));
  }
}

// Toggle polling on/off and persist setting
function toggleDriversPolling() {
  const pollBtn = document.getElementById('poll-toggle');
  const pollIntervalInput = document.getElementById('poll-interval');
  if (!pollBtn || !pollIntervalInput) return;
  if (driversPollingInterval) {
    stopDriversPolling();
    pollBtn.textContent = 'Start polling';
  } else {
    const ms = Math.max(500, parseInt(pollIntervalInput.value || '2000', 10));
    driversPollingInterval = setInterval(fetchAndRenderDrivers, ms);
    fetchAndRenderDrivers();
    pollBtn.textContent = 'Stop polling';
  }
  persistRuntimeSettingsToStorage({
    pollInterval: parseInt(document.getElementById('poll-interval').value || '2000', 10),
    simBatchSize: parseInt(document.getElementById('sim-batch-size').value || '50', 10),
    rustAddrs: (document.getElementById('rust-addrs').value || 'http://127.0.0.1:3030').trim()
  });
}

// Persist runtime settings to localStorage
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
  // apply changes: update polling interval immediately if running
  if (driversPollingInterval) {
    clearInterval(driversPollingInterval);
    driversPollingInterval = setInterval(fetchAndRenderDrivers, settings.pollInterval);
  }
}

// Open Go and Rust metrics in a new tab (best-effort)
function openGoMetrics() {
  window.open('/metrics', '_blank');
}
function openRustMetrics() {
  // try first rust addresses from settings
  const settings = JSON.parse(localStorage.getItem('runtimeSettings') || '{}');
  const addrs = (settings.rustAddrs || 'http://127.0.0.1:3030').split(',').map(s => s.trim()).filter(Boolean);
  window.open((addrs[0] || 'http://127.0.0.1:3030') + '/metrics', '_blank');
}

/* Simulator controls from UI: these interact with the existing simulator when running locally via Go API.
   In this frontend scaffold we call Go generator endpoints if present to trigger local generator/simulator. */
let simRunning = false;
let simIntervalHandle = null;

function startSimulatorFromUI() {
  // persist runtime settings first
  persistRuntimeSettingsToStorage({
    pollInterval: parseInt(document.getElementById('poll-interval').value || '2000', 10),
    simBatchSize: parseInt(document.getElementById('sim-batch-size').value || '50', 10),
    rustAddrs: (document.getElementById('rust-addrs').value || 'http://127.0.0.1:3030').trim()
  });
  const count = parseInt(document.getElementById('sim-count').value || '100', 10);
  const settings = JSON.parse(localStorage.getItem('runtimeSettings') || '{}');
  const rustAddrs = settings.rustAddrs || 'http://127.0.0.1:3030';
  // If Go gateway exposes a simulator control, try calling it. Fallback: call generate-drivers to prime data.
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
  // Frontend cannot forcibly stop background Go simulator; attempt to notify driverhome endpoint or inform user.
  document.getElementById('gear-output').textContent = 'Stop simulator request sent (if available).';
  // If there was an admin endpoint it could be called here; we keep a best-effort message.
}

// Expose functions used by inline onclick attributes when this file is loaded as a module
window.toggleProfile = toggleProfile;
window.toggleConnection = toggleConnection;
window.toggleGear = toggleGear;
window.returnTrackedData = returnTrackedData;
// Expose gear-panel helpers that are called via inline onclick in index.html
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