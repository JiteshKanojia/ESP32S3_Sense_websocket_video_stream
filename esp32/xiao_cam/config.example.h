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
