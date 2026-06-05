// 1. Initialize Cesium Viewer
Cesium.Ion.defaultAccessToken = 'YOUR_CESIUM_ION_TOKEN'; // Get free token from cesium.com

const viewer = new Cesium.Viewer('cesiumContainer', {
    terrainProvider: Cesium.createWorldTerrain(), // High-res global terrain
    baseLayerPicker: false,
    geocoder: false,
    homeButton: false,
    sceneModePicker: false,
    navigationHelpButton: false,
    animation: false,
    timeline: false,
    infoBox: true,
    selectionIndicator: true
});

// Darken the base imagery for a "logistics command center" look
viewer.imageryLayers.get(0).brightness = 0.6;
viewer.imageryLayers.get(0).contrast = 1.2;

// 2. Load Local PLATEAU 3D Tiles
const tilesetUrl = '/tiles/tileset.json'; // Served by nyc-app-server from plateau_3dtiles/

const tileset = viewer.scene.primitives.add(
    new Cesium.Cesium3DTileset({
        url: tilesetUrl,
        maximumScreenSpaceError: 16, // Balance performance/quality
        skipLevelOfDetail: true,     // Crucial for performance with 1000s of buildings
        baseScreenSpaceError: 1024,
        skipScreenSpaceErrorFactor: 16,
        skipLevels: 1,
        immediatelyLoadDesiredLevelOfDetail: false,
        loadSiblings: false
    })
);

// Style the buildings (e.g., make them semi-transparent blue glass)
tileset.style = new Cesium.Cesium3DTileStyle({
    color: {
        conditions: [
            ['${height} >= 100.0', 'color("rgb(40, 80, 150)", 0.8)'], // Skyscrapers
            ['${height} >= 30.0', 'color("rgb(60, 100, 170)", 0.7)'], // Mid-rise
            ['true', 'color("rgb(80, 120, 190)", 0.6)']               // Low-rise
        ]
    },
    show: true
});

tileset.readyPromise.then(() => {
    console.log('✅ PLATEAU 3D Tileset loaded');
    // Center camera on Tokyo
    viewer.camera.flyTo({
        destination: Cesium.Cartesian3.fromDegrees(139.7671, 35.6812, 1500),
        orientation: { heading: Cesium.Math.toRadians(0), pitch: Cesium.Math.toRadians(-45), roll: 0.0 },
        duration: 3
    });
}).otherwise(function (error) {
    console.error('❌ Failed to load tileset:', error);
});

// 3. Advanced Driver Simulation (SampledPositionProperty)
const drivers = [];
const driverCount = 50;

function createDriver(id, startLng, startLat) {
    // Generate a mock path (in production, this is the VRP output from Rust)
    const positions = new Cesium.SampledPositionProperty();
    const startTime = Cesium.JulianDate.now();
    
    // Create 10 waypoints for this driver
    for (let i = 0; i < 10; i++) {
        const time = Cesium.JulianDate.addSeconds(startTime, i * 30, new Cesium.JulianDate());
        const lng = startLng + (Math.random() - 0.5) * 0.02;
        const lat = startLat + (Math.random() - 0.5) * 0.02;
        const height = 50; // Fly slightly above ground to avoid z-fighting with terrain
        
        positions.addSample(time, Cesium.Cartesian3.fromDegrees(lng, lat, height));
    }

    // Make the property loop or extrapolate
    positions.setInterpolationOptions({
        interpolationDegree: 2,
        interpolationAlgorithm: Cesium.LagrangePolynomialApproximation
    });

    const entity = viewer.entities.add({
        id: `driver-${id}`,
        position: positions,
        orientation: new Cesium.VelocityOrientationProperty(positions), // Auto-rotate to face direction of travel!
        model: {
            uri: 'https://raw.githubusercontent.com/KhronosGroup/glTF-Sample-Models/master/2.0/BoxAnimated/glTF-Binary/BoxAnimated.glb', // Replace with delivery van glTF
            scale: 0.5,
            minimumPixelSize: 32
        },
        path: {
            resolution: 1,
            material: new Cesium.PolylineGlowMaterialProperty({
                glowPower: 0.2,
                color: Cesium.Color.CYAN
            }),
            width: 4,
            leadTime: 0,
            trailTime: 60 // Show last 60 seconds of path
        }
    });

    return entity;
}

// Spawn drivers around Tokyo
for (let i = 0; i < driverCount; i++) {
    const lng = 139.70 + Math.random() * 0.12;
    const lat = 35.65 + Math.random() * 0.08;
    drivers.push(createDriver(i, lng, lat));
}

document.getElementById('driver-count').innerText = driverCount;

// 4. UI Controls
function flyToTokyoStation() {
    viewer.camera.flyTo({
        destination: Cesium.Cartesian3.fromDegrees(139.7671, 35.6812, 800),
        orientation: { heading: Cesium.Math.toRadians(-20), pitch: Cesium.Math.toRadians(-60), roll: 0.0 },
        duration: 2
    });
}

// Speed up simulation time
viewer.clock.multiplier = 10; // 10x speed
document.getElementById('sim-speed').innerText = '10x';