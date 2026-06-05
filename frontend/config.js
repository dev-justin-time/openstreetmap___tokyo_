// config.js
export const appConfig = {
  // Map defaults
  TOKYO: { lat: 35.682839, lng: 139.759455, zoom: 13 },
  /* @tweakable [debounce time in ms before calling map.invalidateSize() after zoom/resize to avoid gray tiles] */
  MAP_INVALIDATE_DEBOUNCE_MS: 120,
  /* @tweakable [Interval in ms for forced periodic tile layer refreshing to prevent gray areas (0 to disable)] */
  TILE_LAYER_FORCED_REFRESH_INTERVAL_MS: 5000,

  // GUI & Simulation Settings
  /* @tweakable [Maximum cruise speed in km/h] */
  GUI_MAX_CRUISE_KMPH: 240,
  /* @tweakable [Enable instant acceleration (disables realistic physics)] */
  GUI_INSTANT_ACCEL: false,
  /* @tweakable [Remove speed limit (allow car to go extremely fast)] */
  GUI_NO_SPEED_LIMIT: false,
  /* @tweakable [Disable smart speed detection (e.g., for highways)] */
  GUI_DISABLE_SMART_SPEED: false,
  /* @tweakable [Enable infinite fuel] */
  GUI_INFINITE_FUEL: false,
  /* @tweakable [Invert turn directions in navigation UI (e.g., left becomes right)] */
  GUI_INVERT_TURN_DIRECTIONS: false,
  /* @tweakable [How responsive the car is to curves; higher => slow more on curves] */
  GUI_CURVATURE_SENSITIVITY: 1.2,
  /* @tweakable [Amplitude of natural speed variation (fraction of target speed), e.g. 0.03 = +/-3%] */
  GUI_NATURAL_SPEED_VARIATION: 0.03,
  /* @tweakable [Chance per km of a short micro-brake event (0-1)] */
  GUI_MICRO_BRAKE_PROB_PER_KM: 0.02,
  /* @tweakable [Duration range of a micro-brake in seconds] */
  GUI_MICRO_BRAKE_DURATION_SEC: { min: 0.6, max: 2.5 },
  /* @tweakable [Radius/strength of the driver's yellow glow in CSS units (px)] */
  GUI_DRIVER_GLOW_SIZE_PX: 8,
  /* @tweakable [Color used for drawn route (hex or css color)] */
  GUI_ROUTE_COLOR: "#2b8fff",
  /* @tweakable [Weight (thickness) of the drawn route in pixels] */
  GUI_ROUTE_WEIGHT_PX: 6,
  /* @tweakable [Enable turbo mode that requests multiple route alternatives when planning] */
  GUI_TURBO_MODE: false,
  /* @tweakable [Max alternatives requested when turbo mode is enabled] */
  TURBO_MAX_ALTERNATIVES: 3,
  /* @tweakable [Turbo strictness (0-1) biasing duration vs distance when selecting best alternative] */
  TURBO_STRICTNESS: 0.85,
  /* @tweakable [Toggle showing ETA in HUD] */
  GUI_SHOW_ETA: true,
  /* @tweakable [Minimum speed (m/s) under which ETA falls back to route duration estimate] */
  GUI_ETA_MIN_SPEED_MPS: 0.5,
  /* @tweakable [How to round ETA minutes; 'round'|'ceil'|'floor'] */
  GUI_ETA_MINUTES_ROUNDING: "round",
  /* @tweakable [Toggle showing remaining distance in HUD] */
  GUI_SHOW_REMAINING_DISTANCE: true,
  /* @tweakable [Minimum speed (km/h) to force on segments detected as highway/motorway] */
  GUI_HIGHWAY_MIN_KMPH: 80,
  /* @tweakable [Toggle Imperial units (mph/mi/gal) in HUD and labels] */
  GUI_IMPERIAL: false,
  /* @tweakable [Toggle showing the current country name in the HUD] */
  GUI_SHOW_COUNTRY: true,
  /* @tweakable [Toggle visibility of the HUD/info panel (true = visible)] */
  GUI_INFO_PANEL_VISIBLE: true,
  /* @tweakable [Debounce time in ms for reverse geocoding calls to update country display] */
  COUNTRY_REVERSE_GEOCODE_DEBOUNCE_MS: 5000,
  /* @tweakable [Minimum distance in meters the car must move before re-fetching country name] */
  COUNTRY_REVERSE_GEOCODE_MIN_DIST_M: 1000,
  /* @tweakable [Multiplier applied to segment speed when highway is detected (higher => faster on highways)] */
  HIGHWAY_SPEED_MULTIPLIER: 1.6,
  /* @tweakable [Minimum segment km/h to consider it a highway-like fast segment] */
  HIGHWAY_DETECT_MIN_KMPH: 80,
  /* @tweakable [Minimum segment length (meters) to consider for highway detection] */
  HIGHWAY_MIN_SEGMENT_LENGTH_M: 50,
  /* @tweakable [Global speed realism multiplier; >1 = faster/less conservative speeds] */
  GUI_SPEED_REALISM_MULTIPLIER: 1.3,
  /* @tweakable [Set max acceleration (m/s^2) — higher = faster acceleration] */
  MAX_ACCEL: 6.0,
  /* @tweakable [Set max deceleration (m/s^2) — higher = stronger braking] */
  MAX_DECEL: 8.0,
  /* @tweakable [Fuel consumption base in L/100km] */
  FUEL_CONSUMPTION_BASE: 8,
  /* @tweakable [Fuel tank capacity in liters] */
  FUEL_TANK_CAPACITY: 50,

  // Turn UI
  TOP_TURN_PLACE_MAX_CHARS: 36,
  TOP_TURN_SHOW_PLACE: true,
  TOP_TURN_KM_THRESHOLD: 1000,
  TOP_TURN_ICON_SIZE_PX: 20,

  // Incidents
  /* @tweakable [Whether car incidents can occur (true = enabled)] */
  GUI_INCIDENTS_ENABLED: false,
  /* @tweakable [Chance per km of an incident occurring (0-1); GUI shows percent] */
  INCIDENT_PROBABILITY_PER_KM: 0.02,

  // UI Labels (English default)
  UI_LABELS: {
    connect: "Connect",
    disconnect: "Disconnect",
    myProfile: "My Profile",
    progressTo: "Progress towards",
    silver: "Silver",
    connectionHistory: "Connection History",
    whatToTrack: "What to track",
    assignments: "Assignments",
    runtime: "Runtime",
    noDrivers: "No drivers available",
    selectDriver: "Select driver\u2026",
    selectDriverToAssign: "Select a driver to assign.",
    geolocationUnavailable: "Geolocation not available in your browser",
    geolocationError: "Error getting location: ",
    speed: "Speed",
    fuel: "Fuel",
    country: "Country",
    money: "Money",
    route: "Route",
    type: "Type",
    remaining: "Remaining",
    eta: "ETA",
    follow: "Follow",
    on: "on",
    off: "off",
    hide: "Hide",
    show: "Show",
    startPolling: "Start polling",
    stopPolling: "Stop polling",
    kmph: "KMPH",
    mph: "MPH",
    liters: "L",
    gallons: "gal",
    km: "km",
    m: "m",
    mi: "mi",
    ft: "ft",
    min: "min",
    hr: "hr",
  },

  // Confirm Modal
  CONFIRM_TITLE: "Are you sure?",
  CONFIRM_BODY: "A route already exists. Creating a new one will remove the current route and any progress. Continue?",
  CONFIRM_AUTO_CANCEL_SEC: 0,

  // Beta Banner
  /* @tweakable [Beta banner full text shown to users; editable to tweak wording] */
  OSM_BETA_BANNER_TEXT: "OpenStreetMap May Turn Gray Due To The High Traffic Of American Using The Website - STILL IN BETA BUGS ARE EXPECTED",
  /* @tweakable [Toggle default visibility of the beta banner (true = visible)] */
  OSM_BETA_BANNER_VISIBLE: true,
};