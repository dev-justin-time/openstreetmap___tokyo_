import { appConfig } from "./config.js";
import { map } from "./map.js";
import * as L from "leaflet";

// ─── SSE client for real-time driver updates ────────────────────────────────

let sseSource = null;
let sseReconnectDelay = 1000;

export function startSSE() {
  if (sseSource) return;
  const rustAddr = localStorage.getItem("rust-addrs") || "http://127.0.0.1:3030";
  sseSource = new EventSource(`${rustAddr}/events`);
  sseSource.onopen = () => {
    sseReconnectDelay = 1000;
  };
  sseSource.onmessage = (evt) => {
    try {
      const update = JSON.parse(evt.data);
      onDriverUpdate(update);
    } catch {}
  };
  sseSource.onerror = () => {
    sseSource.close();
    sseSource = null;
    setTimeout(startSSE, Math.min(sseReconnectDelay, 30000));
    sseReconnectDelay *= 2;
  };
}

export function stopSSE() {
  if (sseSource) { sseSource.close(); sseSource = null; }
}

// ─── Driver marker management from SSE ──────────────────────────────────────

const driverMarkersSSE = new Map();

function onDriverUpdate(update) {
  const id = update.id;
  const lat = update.lat;
  const lon = update.lon;
  const status = update.status || "available";

  let marker = driverMarkersSSE.get(id);
  if (!marker) {
    const color = status === "available" ? "#00ff88" : "#ff5252";
    const icon = L.divIcon({
      html: `<div style="width:12px;height:12px;border-radius:50%;background:${color};box-shadow:0 0 6px ${color};"></div>`,
      className: "",
      iconSize: [12, 12],
      iconAnchor: [6, 6],
    });
    marker = L.marker([lat, lon], { icon }).addTo(map);
    marker._driverId = id;
    marker.bindTooltip(id, { direction: "top", offset: L.point(0, -8) });
    driverMarkersSSE.set(id, marker);
  } else {
    marker.setLatLng([lat, lon]);
    const color = status === "available" ? "#00ff88" : "#ff5252";
    marker.setIcon(L.divIcon({
      html: `<div style="width:12px;height:12px;border-radius:50%;background:${color};box-shadow:0 0 6px ${color};"></div>`,
      className: "",
      iconSize: [12, 12],
      iconAnchor: [6, 6],
    }));
  }
}

// ─── Order layer ────────────────────────────────────────────────────────────

const orderMarkers = [];
const LOGISTICS_API = "http://127.0.0.1:8082";

export function setAPIKey(key) {
  localStorage.setItem("logistics-api-key", key);
}

function apiHeaders() {
  const headers = { "Content-Type": "application/json" };
  const key = localStorage.getItem("logistics-api-key");
  if (key) headers["X-API-Key"] = key;
  return headers;
}

// Create a new order
export async function createOrder(pickupLat, pickupLon, dropoffLat, dropoffLon, pickupAddr, dropoffAddr) {
  const res = await fetch(`${LOGISTICS_API}/api/orders`, {
    method: "POST",
    headers: apiHeaders(),
    body: JSON.stringify({
      pickup_lat: pickupLat,
      pickup_lon: pickupLon,
      dropoff_lat: dropoffLat,
      dropoff_lon: dropoffLon,
      pickup_addr: pickupAddr,
      dropoff_addr: dropoffAddr,
    }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// List orders
export async function listOrders(status) {
  const url = status ? `${LOGISTICS_API}/api/orders?status=${status}` : `${LOGISTICS_API}/api/orders`;
  const res = await fetch(url, { headers: apiHeaders() });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// Dispatch order
export async function dispatchOrder(orderId) {
  const res = await fetch(`${LOGISTICS_API}/api/dispatch`, {
    method: "POST",
    headers: apiHeaders(),
    body: JSON.stringify({ order_id: orderId }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// Update order status
export async function updateOrderStatus(orderId, status, driverId) {
  const res = await fetch(`${LOGISTICS_API}/api/orders/${orderId}/status`, {
    method: "PUT",
    headers: apiHeaders(),
    body: JSON.stringify({ status, driver_id: driverId }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// Render order markers on the map
export function renderOrders(orders) {
  // Clear previous
  orderMarkers.forEach(m => map.removeLayer(m));
  orderMarkers.length = 0;

  orders.forEach(o => {
    const color = o.status === "pending" ? "#ffb700"
      : o.status === "assigned" ? "#00d4ff"
      : o.status === "delivered" ? "#00ff88"
      : "#888";

    // Pickup marker
    const pickupIcon = L.divIcon({
      html: `<div style="width:10px;height:10px;border-radius:50%;background:${color};border:2px solid white;"></div>`,
      className: "",
      iconSize: [14, 14],
      iconAnchor: [7, 7],
    });
    const pm = L.marker([o.pickup_lat, o.pickup_lon], { icon: pickupIcon })
      .addTo(map)
      .bindTooltip(`Order ${o.id.slice(-6)}: ${o.status}`, { direction: "top" });
    pm._orderId = o.id;
    pm._type = "pickup";
    orderMarkers.push(pm);

    // Dropoff marker (smaller, diamond)
    if (o.dropoff_lat && o.dropoff_lon) {
      const dropIcon = L.divIcon({
        html: `<div style="width:8px;height:8px;background:${color};transform:rotate(45deg);border:1px solid white;"></div>`,
        className: "",
        iconSize: [10, 10],
        iconAnchor: [5, 5],
      });
      const dm = L.marker([o.dropoff_lat, o.dropoff_lon], { icon: dropIcon })
        .addTo(map)
        .bindTooltip(`Dropoff ${o.id.slice(-6)}`, { direction: "bottom" });
      dm._orderId = o.id;
      dm._type = "dropoff";
      orderMarkers.push(dm);
    }
  });
}

// ─── Dispatch panel UI ──────────────────────────────────────────────────────

let dispatchPanelEl = null;

export function initDispatchPanel() {
  if (dispatchPanelEl) return;

  dispatchPanelEl = document.createElement("div");
  dispatchPanelEl.id = "dispatch-panel";
  dispatchPanelEl.style.cssText = `
    position:fixed;bottom:10px;left:10px;width:320px;max-height:400px;
    background:rgba(15,15,26,0.95);border:1px solid rgba(0,212,255,0.3);
    border-radius:8px;padding:10px;overflow-y:auto;z-index:1000;
    font-family:system-ui,sans-serif;font-size:13px;color:#e0e0e0;
  `;
  dispatchPanelEl.innerHTML = `
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px;">
      <strong style="color:#00d4ff;">Orders</strong>
      <span>
        <button id="dp-refresh" style="background:#2a2a3e;border:1px solid #444;color:#e0e0e0;border-radius:4px;padding:2px 8px;cursor:pointer;">↻</button>
        <button id="dp-create" style="background:#00d4ff;border:none;color:#111;border-radius:4px;padding:2px 8px;cursor:pointer;margin-left:4px;">+</button>
      </span>
    </div>
    <div id="dp-list" style="max-height:300px;overflow-y:auto;"></div>
  `;
  document.body.appendChild(dispatchPanelEl);

  document.getElementById("dp-refresh").onclick = refreshDispatchPanel;
  document.getElementById("dp-create").onclick = showCreateOrderForm;
  refreshDispatchPanel();
  // Auto-refresh every 10s
  setInterval(refreshDispatchPanel, 10000);
}

async function refreshDispatchPanel() {
  const listEl = document.getElementById("dp-list");
  if (!listEl) return;
  try {
    const data = await listOrders();
    renderOrders(data.orders || []);
    listEl.innerHTML = (data.orders || []).map(o => `
      <div style="padding:6px 4px;border-bottom:1px solid rgba(255,255,255,0.05);display:flex;justify-content:space-between;align-items:center;">
        <span style="flex:1;">
          <span style="color:${statusColor(o.status)};font-weight:600;">●</span>
          ${o.id.slice(-6)}
        </span>
        <span style="font-size:11px;color:#888;margin:0 8px;">${o.status}</span>
        ${o.status === "pending" ? `<button class="dp-dispatch" data-id="${o.id}" style="background:#00ff88;border:none;color:#111;border-radius:4px;padding:2px 6px;cursor:pointer;font-size:11px;">Assign</button>` : ""}
        ${o.status === "assigned" ? `<button class="dp-deliver" data-id="${o.id}" style="background:#ffb700;border:none;color:#111;border-radius:4px;padding:2px 6px;cursor:pointer;font-size:11px;">Done</button>` : ""}
      </div>
    `).join("");

    // Wire dispatch buttons
    listEl.querySelectorAll(".dp-dispatch").forEach(btn => {
      btn.onclick = async () => {
        try {
          const result = await dispatchOrder(btn.dataset.id);
          refreshDispatchPanel();
        } catch (e) {
          console.warn("Dispatch failed:", e);
        }
      };
    });
    listEl.querySelectorAll(".dp-deliver").forEach(btn => {
      btn.onclick = async () => {
        try {
          await updateOrderStatus(btn.dataset.id, "delivered", "");
          refreshDispatchPanel();
        } catch (e) {
          console.warn("Delivery failed:", e);
        }
      };
    });
  } catch (e) {
    listEl.innerHTML = `<span style="color:#ff5252;">Connection error</span>`;
  }
}

function statusColor(status) {
  switch (status) {
    case "pending": return "#ffb700";
    case "assigned": return "#00d4ff";
    case "picked_up": return "#8888ff";
    case "delivered": return "#00ff88";
    case "cancelled": return "#ff5252";
    default: return "#888";
  }
}

function showCreateOrderForm() {
  const existing = document.getElementById("dp-create-form");
  if (existing) { existing.remove(); return; }
  const form = document.createElement("div");
  form.id = "dp-create-form";
  form.style.cssText = "margin-top:8px;padding:8px;background:#1a1a2e;border-radius:6px;";
  form.innerHTML = `
    <div style="font-size:11px;color:#888;margin-bottom:4px;">Click map for pickup, then dropoff</div>
    <input id="dp-pickup" placeholder="Pickup lat,lng (or click map)" style="width:100%;background:#2a2a3e;border:1px solid #444;color:#e0e0e0;border-radius:4px;padding:4px 6px;margin-bottom:4px;font-size:12px;">
    <input id="dp-dropoff" placeholder="Dropoff lat,lng (or click map)" style="width:100%;background:#2a2a3e;border:1px solid #444;color:#e0e0e0;border-radius:4px;padding:4px 6px;margin-bottom:4px;font-size:12px;">
    <div style="display:flex;gap:4px;">
      <button id="dp-submit-order" style="flex:1;background:#00d4ff;border:none;color:#111;border-radius:4px;padding:4px;cursor:pointer;font-size:12px;">Create</button>
      <button id="dp-cancel-form" style="background:#444;border:none;color:#e0e0e0;border-radius:4px;padding:4px;cursor:pointer;font-size:12px;">Cancel</button>
    </div>
  `;
  dispatchPanelEl.appendChild(form);

  document.getElementById("dp-cancel-form").onclick = () => form.remove();

  // Click map to set coords
  let pickStage = 0;
  const clickHandler = (e) => {
    const ll = e.latlng;
    if (pickStage === 0) {
      document.getElementById("dp-pickup").value = `${ll.lat.toFixed(5)},${ll.lng.toFixed(5)}`;
      pickStage = 1;
    } else {
      document.getElementById("dp-dropoff").value = `${ll.lat.toFixed(5)},${ll.lng.toFixed(5)}`;
      pickStage = 0;
      map.off("click", clickHandler);
    }
  };
  // Use map.click once, then revert
  map.once("click", clickHandler);
  // But allow manual click if the user clicks twice on the form button
  document.getElementById("dp-submit-order").onclick = async () => {
    const pVal = document.getElementById("dp-pickup").value.trim();
    const dVal = document.getElementById("dp-dropoff").value.trim();
    if (!pVal || !dVal) return;
    const p = pVal.split(",").map(Number);
    const dp = dVal.split(",").map(Number);
    if (p.length < 2 || dp.length < 2 || isNaN(p[0]) || isNaN(dp[0])) return;
    try {
      await createOrder(p[0], p[1], dp[0], dp[1]);
      form.remove();
      refreshDispatchPanel();
    } catch (e) {
      console.warn("Create order failed:", e);
    }
  };
}

// ─── Keyboard shortcut ──────────────────────────────────────────────────────

document.addEventListener("keydown", (e) => {
  if (e.key === "o" && !e.ctrlKey && !e.metaKey) {
    e.preventDefault();
    if (!dispatchPanelEl) {
      initDispatchPanel();
    } else {
      dispatchPanelEl.style.display = dispatchPanelEl.style.display === "none" ? "" : "none";
    }
  }
});
