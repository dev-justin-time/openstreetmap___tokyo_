export const APP_STATE = {
  // shared driver markers map keyed by driver id
  driverMarkers: {},
  // shared Leaflet layer group (set by initMap)
  driversLayerGroup: null,
  // runtime settings (persisted)
  runtimeSettings: {
    pollInterval: 2000,
    simBatchSize: 50,
    rustAddrs: 'http://127.0.0.1:3030'
  }
};

// helper to get current driver ids
export function getDriverIds() {
  return Object.keys(APP_STATE.driverMarkers || {});
}

export function registerMarker(id, marker) {
  APP_STATE.driverMarkers[id] = marker;
}

export function removeMarker(id) {
  if (APP_STATE.driverMarkers && APP_STATE.driverMarkers[id]) {
    try {
      APP_STATE.driversLayerGroup.removeLayer(APP_STATE.driverMarkers[id]);
    } catch (e) {}
    delete APP_STATE.driverMarkers[id];
  }
}

export function setDriversLayerGroup(group) {
  APP_STATE.driversLayerGroup = group;
}

export function getDriversLayerGroup() {
  return APP_STATE.driversLayerGroup;
}

// runtime settings helpers
export function loadRuntimeSettingsFromStorage() {
  try {
    const settings = JSON.parse(localStorage.getItem('runtimeSettings') || '{}');
    APP_STATE.runtimeSettings.pollInterval = settings.pollInterval || APP_STATE.runtimeSettings.pollInterval;
    APP_STATE.runtimeSettings.simBatchSize = settings.simBatchSize || APP_STATE.runtimeSettings.simBatchSize;
    APP_STATE.runtimeSettings.rustAddrs = settings.rustAddrs || APP_STATE.runtimeSettings.rustAddrs;
  } catch (e) {
    // ignore
  }
  return APP_STATE.runtimeSettings;
}

export function persistRuntimeSettingsToStorage(settings) {
  try {
    localStorage.setItem('runtimeSettings', JSON.stringify(settings));
    APP_STATE.runtimeSettings = Object.assign({}, APP_STATE.runtimeSettings, settings);
  } catch (e) {
    console.warn('persistRuntimeSettings error', e);
  }
}