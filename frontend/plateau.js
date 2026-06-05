
// ============================================================
// Tokyo Logistics — 3D PLATEAU Integration
// ============================================================

// --- Configuration ---
const CONFIG = {
    // PLATEAU 3D Tiles endpoints (Tokyo 23-ku)
    // These are the official MLIT PLATEAU asset URLs
    plateau: {
        // Tokyo 23-ku building tiles (3D Tiles format)
        buildings: 'https://assets.cms.plateau.reearth.io/assets/3d/tiles/23ku/tileset.json',
        // Alternative: Use the unified PLATEAU endpoint
        // buildings_v2: 'https://gisservices.cms.plateau.reearth.io/api/plateau/v1/experimental/feature/13100/tiles/3dtiles/{z}/{x}/{y}.pnts'
    },
    
    // Tokyo center coordinates
    tokyo: {
        center: [139.7671, 35.6812], // Tokyo Station
        zoom: 14.5,
        pitch: 60,
        bearing: -17.6
    },

    // Named camera presets
    cameraPresets: {
        'tokyo-station': { center: [139.7671, 35.6812], zoom: 16, pitch: 60, bearing: -20 },
        'shibuya':       { center: [139.7016, 35.6580], zoom: 16, pitch: 65, bearing: 30 },
        'shinjuku':      { center: [139.7005, 35.6896], zoom: 16, pitch: 60, bearing: -45 },
        'birds-eye':     { center: [139.7500, 35.6800], zoom: 12, pitch: 0, bearing: 0 }
    },

    // Simulation
    simulation: {
        speed: 1,
        paused: false,
        driverCount: 50
    }
};

// --- Map Initialization ---
const map = new maplibregl.Map({
    container: 'map3d',
    style: {
        version: 8,
        name: 'Tokyo Dark',
        // Use GSI (Geospatial Information Authority of Japan) standard map
        sources: {
            'gsi-std': {
                type: 'raster',
                tiles: [
                    'https://cyberjapandata.gsi.go.jp/xyz/std/{z}/{x}/{y}.png'
                ],
                tileSize: 256,
                attribution: '<a href="https://maps.gsi.go.jp/development/ichiran.html">GSI Japan</a>'
            },
            'gsi-pale': {
                type: 'raster',
                tiles: [
                    'https://cyberjapandata.gsi.go.jp/xyz/pale/{z}/{x}/{y}.png'
                ],
                tileSize: 256,
                attribution: 'GSI Japan'
            }
        },
        layers: [
            {
                id: 'gsi-base',
                type: 'raster',
                source: 'gsi-pale',
                paint: { 'raster-opacity': 0.85 }
            }
        ],
        glyphs: 'https://demotiles.maplibre.org/font/{fontstack}/{range}.pbf',
        sprite: 'https://demotiles.maplibre.org/styles/osm-bright-style/sprite'
    },
    center: CONFIG.tokyo.center,
    zoom: CONFIG.tokyo.zoom,
    pitch: CONFIG.tokyo.pitch,
    bearing: CONFIG.tokyo.bearing,
    maxPitch: 80,
    antialias: true
});

// --- Navigation Controls ---
map.addControl(new maplibregl.NavigationControl({ visualizePitch: true }), 'top-left');
map.addControl(new maplibregl.ScaleControl({ maxWidth: 150 }), 'bottom-center');

// ============================================================
// MAP LOAD — Add PLATEAU 3D Tiles & Terrain
// ============================================================
map.on('load', async () => {
    console.log('🗺️ Map loaded. Adding PLATEAU 3D layers...');

    // --- 1. Add Terrain (DEM from GSI) ---
    try {
        map.addSource('terrain-dem', {
            type: 'raster-dem',
            // GSI DEM from PLATEAU
            tiles: ['https://assets.cms.plateau.reearth.io/assets/terrain/dem/{z}/{x}/{y}.png'],
            tileSize: 256,
            maxzoom: 15
        });
        map.setTerrain({ source: 'terrain-dem', exaggeration: 1.2 });
        console.log('✅ Terrain added');
    } catch (e) {
        console.warn('⚠️ Terrain source unavailable, using flat terrain:', e.message);
    }

    // --- 2. Add PLATEAU 3D Buildings ---
    try {
        map.addSource('plateau-buildings', {
            type: 'vector',
            url: 'https://assets.cms.plateau.reearth.io/assets/3d/tiles/23ku/tileset.json'
        });

        // Note: PLATEAU tiles may use 'mesh' or 'building' layer names
        // Adjust based on actual tileset structure
        map.addLayer({
            id: 'plateau-3d-buildings',
            source: 'plateau-buildings',
            'source-layer': 'building', // may need adjustment
            type: 'fill-extrusion',
            minzoom: 13,
            paint: {
                'fill-extrusion-color': [
                    'interpolate', ['linear'], ['get', 'measuredHeight'],
                    0,   '#1a237e',  // Low buildings — deep blue
                    30,  '#283593',  // Medium
                    60,  '#3949ab',  // Tall
                    150, '#5c6bc0',  // Skyscrapers
                    300, '#7986cb'   // Very tall (Toranomon, etc.)
                ],
                'fill-extrusion-height': [
                    'interpolate', ['linear'], ['zoom'],
                    13, 0,
                    15, ['get', 'measuredHeight']
                ],
                'fill-extrusion-base': ['get', 'measuredHeight'], // or 0
                'fill-extrusion-opacity': 0.85
            }
        });
        console.log('✅ PLATEAU 3D buildings added');
    } catch (e) {
        console.warn('⚠️ PLATEAU vector tiles failed, trying alternative method...', e.message);
        // Fallback: Use MapLibre's built-in OpenMapTiles buildings
        addFallbackBuildings();
    }

    // --- 3. Add Sky Layer ---
    try {
        map.setFog({
            color: 'rgb(220, 230, 250)',
            'high-color': 'rgb(36, 92, 200)',
            'horizon-blend': 0.04,
            'space-color': 'rgb(11, 11, 25)',
            'star-intensity': 0.35
        });
    } catch (e) {
        console.warn('⚠️ Fog/sky not supported');
    }

    // --- 4. Add Simulation Layers ---
    addSimulationLayers();

    // --- 5. Load Initial Simulation Data ---
    loadSimulationData();

    // Hide loading screen
    setTimeout(() => {
        document.getElementById('loading').style.opacity = '0';
        setTimeout(() => document.getElementById('loading').style.display = 'none', 500);
    }, 1000);
});

// ============================================================
// FALLBACK: Use OSM Buildings if PLATEAU fails
// ============================================================
function addFallbackBuildings() {
    console.log('🔄 Adding fallback 3D buildings from OpenMapTiles...');
    
    map.addSource('openmaptiles', {
        type: 'vector',
        url: 'https://demotiles.maplibre.org/tiles/tiles.json'
    });

    map.addLayer({
        id: '3d-buildings',
        source: 'openmaptiles',
        'source-layer': 'building',
        type: 'fill-extrusion',
        minzoom: 14,
        paint: {
            'fill-extrusion-color': '#aaa',
            'fill-extrusion-height': ['get', 'render_height'],
            'fill-extrusion-base': ['get', 'render_min_height'],
            'fill-extrusion-opacity': 0.7
        }
    });
}

// ============================================================
// SIMULATION LAYERS — Drivers, Routes, Heatmap
// ============================================================
function addSimulationLayers() {
    // --- Driver Points ---
    map.addSource('drivers', {
        type: 'geojson',
        data: { type: 'FeatureCollection', features: [] }
    });

    map.addLayer({
        id: 'drivers-layer',
        type: 'circle',
        source: 'drivers',
        paint: {
            'circle-radius': 6,
            'circle-color': [
                'match', ['get', 'status'],
                'idle', '#4caf50',
                'en-route', '#ff9800',
                'delivering', '#f44336',
                '#9e9e9e'
            ],
            'circle-stroke-width': 2,
            'circle-stroke-color': '#ffffff',
            'circle-emissive-strength': 0.5
        }
    });

    // --- Driver Routes (LineStrings) ---
    map.addSource('routes', {
        type: 'geojson',
        data: { type: 'FeatureCollection', features: [] }
    });

    map.addLayer({
        id: 'routes-layer',
        type: 'line',
        source: 'routes',
        paint: {
            'line-color': [
                'match', ['get', 'urgency'],
                'high', '#f44336',
                'medium', '#ff9800',
                'low', '#4caf50',
                '#2196f3'
            ],
            'line-width': 3,
            'line-opacity': 0.7,
            'line-dasharray': [2, 1]
        }
    });

    // --- Order Heatmap ---
    map.addSource('orders-heatmap', {
        type: 'geojson',
        data: { type: 'FeatureCollection', features: [] }
    });

    map.addLayer({
        id: 'orders-heatmap-layer',
        type: 'heatmap',
        source: 'orders-heatmap',
        maxzoom: 17,
        paint: {
            'heatmap-weight': ['get', 'weight'],
            'heatmap-intensity': [
                'interpolate', ['linear'], ['zoom'],
                10, 0.5,
                16, 3
            ],
            'heatmap-radius': [
                'interpolate', ['linear'], ['zoom'],
                10, 8,
                16, 30
            ],
            'heatmap-color': [
                'interpolate', ['linear'], ['heatmap-density'],
                0,   'rgba(0,0,255,0)',
                0.2, 'rgb(0,128,255)',
                0.4, 'rgb(0,255,128)',
                0.6, 'rgb(255,255,0)',
                0.8, 'rgb(255,128,0)',
                1,   'rgb(255,0,0)'
            ],
            'heatmap-opacity': 0.6
        },
        layout: { visibility: 'none' }
    });

    // --- Click interaction for drivers ---
    map.on('click', 'drivers-layer', (e) => {
        if (e.features.length > 0) {
            const feature = e.features[0];
            showDriverPanel(feature.properties);
        }
    });

    map.on('mouseenter', 'drivers-layer', () => {
        map.getCanvas().style.cursor = 'pointer';
    });
    map.on('mouseleave', 'drivers-layer', () => {
        map.getCanvas().style.cursor = '';
    });
}

// ============================================================
// SIMULATION DATA — Mock & SSE Integration
// ============================================================
let simulationData = {
    drivers: [],
    routes: [],
    orders: []
};

function loadSimulationData() {
    // Generate mock drivers around Tokyo
    const drivers = generateMockDrivers(CONFIG.simulation.driverCount);
    simulationData.drivers = drivers;
    updateDriversOnMap(drivers);

    // Generate mock orders
    const orders = generateMockOrders(20);
    simulationData.orders = orders;
    updateHeatmap(orders);

    // Generate mock routes
    const routes = generateMockRoutes(drivers, orders);
    simulationData.routes = routes;
    updateRoutesOnMap(routes);

    // Update HUD
    updateHUD(drivers.length, orders.length);

    // Start simulation loop
    startSimulation();
}

function generateMockDrivers(count) {
    const drivers = [];
    const statuses = ['idle', 'en-route', 'delivering'];
    
    for (let i = 0; i < count; i++) {
        // Scatter around central Tokyo
        const lng = 139.70 + Math.random() * 0.12;
        const lat = 35.65 + Math.random() * 0.08;
        
        drivers.push({
            id: `driver-${i}`,
            name: `Driver ${i + 1}`,
            coordinates: [lng, lat],
            status: statuses[Math.floor(Math.random() * statuses.length)],
            speed: 20 + Math.random() * 30, // km/h
            heading: Math.random() * 360,
            fuel: 40 + Math.random() * 60,
            earnings: Math.floor(Math.random() * 15000),
            deliveries: Math.floor(Math.random() * 20)
        });
    }
    return drivers;
}

function generateMockOrders(count) {
    const orders = [];
    for (let i = 0; i < count; i++) {
        orders.push({
            id: `order-${i}`,
            coordinates: [
                139.70 + Math.random() * 0.12,
                35.65 + Math.random() * 0.08
            ],
            weight: Math.random() * 3 + 0.5
        });
    }
    return orders;
}

function generateMockRoutes(drivers, orders) {
    const routes = [];
    const enRouteDrivers = drivers.filter(d => d.status === 'en-route');
    
    enRouteDrivers.forEach((driver, idx) => {
        if (idx < orders.length) {
            const order = orders[idx % orders.length];
            // Create a simple route (in production, this comes from OSRM)
            routes.push({
                id: `route-${driver.id}`,
                driverId: driver.id,
                orderId: order.id,
                coordinates: [
                    driver.coordinates,
                    // Add midpoint for visual curve
                    [
                        (driver.coordinates[0] + order.coordinates[0]) / 2 + (Math.random() - 0.5) * 0.01,
                        (driver.coordinates[1] + order.coordinates[1]) / 2 + (Math.random() - 0.5) * 0.01
                    ],
                    order.coordinates
                ],
                urgency: ['high', 'medium', 'low'][Math.floor(Math.random() * 3)]
            });
        }
    });
    return routes;
}

// ============================================================
// MAP UPDATES
// ============================================================
function updateDriversOnMap(drivers) {
    const geojson = {
        type: 'FeatureCollection',
        features: drivers.map(d => ({
            type: 'Feature',
            geometry: { type: 'Point', coordinates: d.coordinates },
            properties: d
        }))
    };
    map.getSource('drivers').setData(geojson);
}

function updateRoutesOnMap(routes) {
    const geojson = {
        type: 'FeatureCollection',
        features: routes.map(r => ({
            type: 'Feature',
            geometry: { type: 'LineString', coordinates: r.coordinates },
            properties: r
        }))
    };
    map.getSource('routes').setData(geojson);
}

function updateHeatmap(orders) {
    const geojson = {
        type: 'FeatureCollection',
        features: orders.map(o => ({
            type: 'Feature',
            geometry: { type: 'Point', coordinates: o.coordinates },
            properties: { weight: o.weight }
        }))
    };
    map.getSource('orders-heatmap').setData(geojson);
}

function updateHUD(driverCount, orderCount) {
    document.getElementById('driver-count').textContent = driverCount;
    document.getElementById('order-count').textContent = orderCount;
    
    // Calculate average ETA (mock)
    const avgEta = (8 + Math.random() * 12).toFixed(1);
    document.getElementById('avg-eta').textContent = `${avgEta} min`;
}

// ============================================================
// SIMULATION LOOP — Animate drivers
// ============================================================
let animationFrame;

function startSimulation() {
    function tick() {
        if (!CONFIG.simulation.paused) {
            // Move each driver slightly
            simulationData.drivers.forEach(driver => {
                if (driver.status !== 'idle') {
                    const speed = driver.speed * 0.00001 * CONFIG.simulation.speed;
                    const rad = (driver.heading * Math.PI) / 180;
                    driver.coordinates[0] += Math.cos(rad) * speed;
                    driver.coordinates[1] += Math.sin(rad) * speed;
                    
                    // Add slight heading variation
                    driver.heading += (Math.random() - 0.5) * 5;
                }
            });
            
            updateDriversOnMap(simulationData.drivers);
        }
        
        animationFrame = requestAnimationFrame(tick);
    }
    tick();
}

// ============================================================
// UI CONTROLS
// ============================================================

// --- Camera Presets ---
Object.entries(CONFIG.cameraPresets).forEach(([key, preset]) => {
    const btnId = `cam-${key}`;
    const btn = document.getElementById(btnId);
    if (btn) {
        btn.addEventListener('click', () => {
            map.flyTo({
                center: preset.center,
                zoom: preset.zoom,
                pitch: preset.pitch,
                bearing: preset.bearing,
                duration: 2000,
                essential: true
            });
        });
    }
});

// --- Layer Toggles ---
document.getElementById('toggle-buildings').addEventListener('change', (e) => {
    const visibility = e.target.checked ? 'visible' : 'none';
    if (map.getLayer('plateau-3d-buildings')) {
        map.setLayoutProperty('plateau-3d-buildings', 'visibility', visibility);
    }
    if (map.getLayer('3d-buildings')) {
        map.setLayoutProperty('3d-buildings', 'visibility', visibility);
    }
});

document.getElementById('toggle-terrain').addEventListener('change', (e) => {
    if (e.target.checked) {
        map.setTerrain({ source: 'terrain-dem', exaggeration: 1.2 });
    } else {
        map.setTerrain(null);
    }
});

document.getElementById('toggle-heatmap').addEventListener('change', (e) => {
    const visibility = e.target.checked ? 'visible' : 'none';
    map.setLayoutProperty('orders-heatmap-layer', 'visibility', visibility);
});

document.getElementById('toggle-routes').addEventListener('change', (e) => {
    const visibility = e.target.checked ? 'visible' : 'none';
    map.setLayoutProperty('routes-layer', 'visibility', visibility);
});

// --- Simulation Controls ---
document.getElementById('btn-play').addEventListener('click', () => {
    CONFIG.simulation.paused = false;
});

document.getElementById('btn-pause').addEventListener('click', () => {
    CONFIG.simulation.paused = true;
});

document.getElementById('btn-reset').addEventListener('click', () => {
    CONFIG.simulation.paused = true;
    loadSimulationData();
});

document.getElementById('speed-slider').addEventListener('input', (e) => {
    CONFIG.simulation.speed = parseInt(e.target.value);
    document.getElementById('speed-val').textContent = `${CONFIG.simulation.speed}x`;
});

// --- Driver Info Panel ---
function showDriverPanel(props) {
    const panel = document.getElementById('driver-panel');
    const info = document.getElementById('driver-info');
    
    info.innerHTML = `
        <div class="stat"><span class="label">Name</span><span class="value">${props.name}</span></div>
        <div class="stat"><span class="label">Status</span><span class="value">${props.status}</span></div>
        <div class="stat"><span class="label">Speed</span><span class="value">${props.speed.toFixed(1)} km/h</span></div>
        <div class="stat"><span class="label">Fuel</span><span class="value">${props.fuel.toFixed(0)}%</span></div>
        <div class="stat"><span class="label">Earnings</span><span class="value">¥${props.earnings.toLocaleString()}</span></div>
        <div class="stat"><span class="label">Deliveries</span><span class="value">${props.deliveries}</span></div>
    `;
    
    panel.style.display = 'block';
    
    // Fly to driver
    map.flyTo({
        center: props.coordinates,
        zoom: 17,
        pitch: 65,
        duration: 1500
    });
}

// ============================================================
// SSE INTEGRATION — Connect to Go Backend
// ============================================================
// Uncomment and configure when connecting to your Go API gateway:
/*
function connectSSE() {
    const eventSource = new EventSource('http://localhost:8080/api/v1/stream/drivers');
    
    eventSource.onmessage = (event) => {
        const data = JSON.parse(event.data);
        
        if (data.type === 'driver_update') {
            const driver = simulationData.drivers.find(d => d.id === data.driver.id);
            if (driver) {
                driver.coordinates = data.driver.coordinates;
                driver.status = data.driver.status;
                driver.speed = data.driver.speed;
            }
            updateDriversOnMap(simulationData.drivers);
        }
        
        if (data.type === 'new_order') {
            simulationData.orders.push(data.order);
            updateHeatmap(simulationData.orders);
            updateHUD(simulationData.drivers.length, simulationData.orders.length);
        }
        
        if (data.type === 'route_assigned') {
            simulationData.routes.push(data.route);
            updateRoutesOnMap(simulationData.routes);
        }
    };
    
    eventSource.onerror = (err) => {
        console.error('SSE connection error:', err);
        // Reconnect after 5 seconds
        setTimeout(connectSSE, 5000);
    };
}

// Call after map loads
map.on('load', connectSSE);
*/

// ============================================================
// ERROR HANDLING
// ============================================================
map.on('error', (e) => {
    console.error('Map error:', e.error);
});

console.log('🚀 Tokyo 3D Logistics initialized');
console.log('📍 PLATEAU 3D City Model integration active');

