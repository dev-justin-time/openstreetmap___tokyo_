// betaBanner.js
import { appConfig } from "./frontend/config.js";

export function initBetaBanner() {
  const banner = document.getElementById("osm-beta-banner");
  const inner = document.getElementById("osm-beta-inner");
  const close = document.getElementById("osm-beta-close");
  if (!banner || !inner || !close) return;
  inner.textContent = appConfig.OSM_BETA_BANNER_TEXT;
  banner.setAttribute("aria-hidden", appConfig.OSM_BETA_BANNER_VISIBLE ? "false" : "true");
  banner.style.display = appConfig.OSM_BETA_BANNER_VISIBLE ? "flex" : "none";
  close.addEventListener("click", () => {
    banner.style.display = "none";
    banner.setAttribute("aria-hidden", "true");
    appConfig.OSM_BETA_BANNER_VISIBLE = false;
  });
}

