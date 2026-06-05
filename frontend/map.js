// map.js
import * as L from "leaflet";
import { appConfig } from "./config.js";

// Initialize the map
export const map = L.map("map", {
  center: [appConfig.TOKYO.lat, appConfig.TOKYO.lng],
  zoom: appConfig.TOKYO.zoom,
  zoomControl: true,
  attributionControl: true,
  fadeAnimation: true, // Default Leaflet behavior, often desired for smooth tile loading
});

// Add OpenStreetMap tile layer
L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
  maxZoom: 19,
  attribution:
    '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
}).addTo(map);

// Layer for general markers (e.g., destination)
export const markerLayer = L.layerGroup().addTo(map);

export function addMarker(lat, lng, text) {
  markerLayer.clearLayers();
  const m = L.marker([lat, lng]).addTo(markerLayer);
  console.log("Marker:", text || `${lat.toFixed(6)}, ${lng.toFixed(6)}`);
}

// Driver marker (yellow highlight as a circle marker)
export const driverLayer = L.layerGroup().addTo(map);
export const driverMarker = L.circleMarker(
  [appConfig.TOKYO.lat, appConfig.TOKYO.lng],
  {
    radius: 8,
    color: "#f0c419",
    weight: 2,
    fillColor: "#f8e09a",
    fillOpacity: 1,
    interactive: false,
    className: "driver-car driver-glow", // add class for glow styling
  }
).addTo(driverLayer);

// Utility to refresh driver glow CSS variable
export function refreshDriverGlow() {
  try {
    document.documentElement.style.setProperty(
      "--driver-glow-size",
      `${appConfig.GUI_DRIVER_GLOW_SIZE_PX}px`
    );
  } catch (e) {
    console.warn("Error setting driver glow CSS var:", e);
  }
}

let _forcedRefreshIntervalId = null;

// Function to start or stop the periodic forced map refresh
export function setupForcedMapRefresh() {
  if (_forcedRefreshIntervalId) {
    clearInterval(_forcedRefreshIntervalId);
    _forcedRefreshIntervalId = null;
  }

  if (appConfig.TILE_LAYER_FORCED_REFRESH_INTERVAL_MS > 0) {
    _forcedRefreshIntervalId = setInterval(() => {
      try {
        // Invalidate size to ensure map container dimensions are correct, without re-panning or re-zooming
        map.invalidateSize({ pan: false, zoom: false });

        // Force update on all tile layers to re-fetch/redraw tiles
        map.eachLayer((layer) => {
          if (
            layer instanceof L.TileLayer &&
            typeof layer._update === "function"
          ) {
            try {
              layer.redraw();
            } catch (e) {
              /* ignore per-layer errors */
            }
          }
        });
      } catch (e) {
        console.warn("Forced map refresh failed:", e);
      }
    }, appConfig.TILE_LAYER_FORCED_REFRESH_INTERVAL_MS);
  }
}

// Ensure Leaflet container refreshes after zoom/resize to avoid gray tile canvas
export function setupMapRefresh() {
  let invalidateTimer = null;
  function scheduleInvalidate() {
    if (invalidateTimer) clearTimeout(invalidateTimer);
    invalidateTimer = setTimeout(() => {
      try {
        map.invalidateSize({ reset: false });
      } catch (e) {
        /* ignore; defensive */
      }
      map.eachLayer((layer) => {
        if (layer && typeof layer.redraw === "function") {
          try {
            layer.redraw();
          } catch (e) {
            /* ignore per-layer errors */
          }
        }
        try {
          if (layer && layer._tiles && typeof layer._update === "function") {
            // Explicitly call _update for tile layers
            layer.redraw();
          }
        } catch (e) {
          /* ignore */
        }
      });
      if (window._routeLine && typeof window._routeLine.redraw === "function")
        window._routeLine.redraw();
      if (driverMarker && typeof driverMarker.redraw === "function")
        driverMarker.redraw();
      invalidateTimer = null;
    }, appConfig.MAP_INVALIDATE_DEBOUNCE_MS);
  }
  map.on("zoomend", scheduleInvalidate);
  map.on("zoomanim", scheduleInvalidate);
  map.on("moveend", scheduleInvalidate);
  map.on("zoom", () => {
    try {
      if (window._routeLine && typeof window._routeLine.redraw === "function")
        window._routeLine.redraw();
      if (driverMarker && typeof driverMarker.redraw === "function")
        driverMarker.redraw();
      map.eachLayer((layer) => {
        if (layer && typeof layer.redraw === "function") {
          try {
            layer.redraw();
          } catch (e) {
            console.warn("Layer redraw failed", e);
          }
        }
      });
    } catch (e) {
      console.warn("Zoom handler error", e);
    }
  });
  map.on("move", () => {
    try {
      if (window._routeLine && typeof window._routeLine.redraw === "function")
        window._routeLine.redraw();
      if (driverMarker && typeof driverMarker.redraw === "function")
        driverMarker.redraw();
    } catch (e) {
      console.warn("Move handler error", e);
    }
  });
  window.addEventListener("resize", scheduleInvalidate, { passive: true });
}
