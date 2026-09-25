#pragma once

// Copy this file to config.h and fill it in. config.h is gitignored.

#define WIFI_SSID "your-ssid"
#define WIFI_PASSWORD "your-password"

// Host only: tunnel hostname or your PC LAN IP. No scheme, no path.
#define CAM_HOST "your-tunnel.trycloudflare.com"
#define CAM_PATH "/ingest"  // WebSocket binary JPEG ingest (HTTP POST also supported)
// 1 = wss on 443 (Cloudflare). 0 = ws on CAM_PORT (local server on the LAN).
#define CAM_USE_TLS 1
#define CAM_PORT 8080

// Sent as the X-API-Key header. Do not put CR or LF in this value.
#define INGEST_API_KEY "replace-me"

// Mains frequency for OV3660 anti-banding: 50 (EU/IN) or 60 (US). 0 = driver default.
#define CAM_ANTIBANDING_HZ 50

// Wi-Fi TX power (default max). Lower e.g. WIFI_POWER_11dBm if you see vertical stripes on the image.
// #define CAM_WIFI_TX_POWER WIFI_POWER_19_5dBm

// Boot orientation (also adjustable in the viewer). 0 or 1.
// #define CAM_VFLIP 0
// #define CAM_HMIRROR 0
