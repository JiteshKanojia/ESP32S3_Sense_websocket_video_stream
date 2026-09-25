# Luckfox camera stream

Go server for a Luckfox Pico. A Seeed XIAO ESP32S3 Sense pushes JPEG frames to `/ingest`. A browser logs in with a password and watches `/`.

The server keeps one JPEG in memory. Run it on your LAN (PC or Pico) and point the camera at that host over plain HTTP/WebSocket.

## Server

Put `config.env` in the same directory as the executable. `go run` also checks the working directory.

```
VIEW_PASSWORD=choose-a-password
INGEST_API_KEY=choose-an-api-key
SESSION_SECRET=choose-a-long-random-string
LISTEN_ADDR=:8080
```

`LISTEN_ADDR` is optional and defaults to `:8080`. From `server/`:

```powershell
go test ./...
go run ./cmd/server
```

Open `http://127.0.0.1:8080`, log in, and the page streams over **`/watch`** (WebSocket). `GET /frame` remains for simple HTTP clients.

The camera pushes JPEGs over **WebSocket** (`GET /ingest`, binary frames, `X-API-Key` header). **HTTP POST** to `/ingest` still works for `fakecam`. `-url` defaults to `http://127.0.0.1:8080/ingest`.

```powershell
go run ./cmd/fakecam -file C:\path\to\frame.jpg
```

| Route | Who | Auth |
| --- | --- | --- |
| `GET /` | browser | session cookie after `POST /login` |
| `GET /watch` | browser (WebSocket, binary JPEG) | session cookie on handshake |
| `GET /frame` | tools (single JPEG snapshot) | same cookie |
| `GET /camera/settings` | browser (JSON snapshot) | session cookie |
| `PATCH /camera/settings` | browser (partial JSON) | session cookie; server pushes text JSON to camera over ingest WS |
| `GET /ingest` | camera (WebSocket, binary JPEG) | `X-API-Key` on handshake |
| `POST /ingest` | fakecam / tools (`image/jpeg` body) | `X-API-Key` header |

The session cookie is marked `Secure` when the request is HTTPS (e.g. `X-Forwarded-Proto: https` behind a reverse proxy). Login failures are limited to 8 per client IP per minute.

## Luckfox Pico

Cross-compile a static binary on your PC. The Pico (RV1103) is 32-bit ARM:

```powershell
cd server
$env:CGO_ENABLED = "0"
$env:GOOS = "linux"
$env:GOARCH = "arm"
$env:GOARM = "7"
go build -o camserver ./cmd/server
```

Copy `camserver` and `config.env` to the same directory on the board, and run the binary from that directory. Use the board’s LAN IP for the camera and browser.

## ESP32 sketch

Arduino sketch: `esp32/xiao_cam/`. Board: Seeed XIAO ESP32S3 Sense with **OPI PSRAM** (works with **OV3660** or OV2640; `esp_camera` auto-detects the sensor).

1. Copy `esp32/xiao_cam/config.example.h` to `esp32/xiao_cam/config.h`.
2. Set Wi-Fi, the server’s **LAN IP** as `CAM_HOST`, `CAM_USE_TLS` to `0`, `CAM_PORT` to `8080`, and the same ingest API key.
3. Install the Arduino library **WebSockets** by Markus Sattler.
4. Flash `xiao_cam.ino`.

The sketch streams **1024×768 (XGA)** JPEGs over **WebSocket** (`ws://HOST:PORT/ingest`). Boot defaults are in `xiao_cam.ino` (`kJpegQuality`, `kXclkHz`, etc.). After login, use **Camera tuning** on the viewer page to change quality, anti-banding, denoise, lens correction, sharpness, and flip/mirror at runtime (debounced `PATCH` → small text frame on the ingest WebSocket; the ESP32 applies once per message, not per frame).

**Arduino Tools (XIAO ESP32S3 Sense):** set **PSRAM** to **OPI PSRAM**. Camera init runs **before Wi-Fi** on XIAO (malloc requirement).

Allow inbound TCP **8080** on the machine running the server if the firewall blocks LAN clients.

### Frame rate (theory vs this project)

Per [Espressif’s camera FAQ](https://docs.espressif.com/projects/esp-faq/en/latest/application-solution/camera-application.html):

- **Capture** scales with resolution, JPEG size, and XCLK/PCLK (ESP32-S3 PCLK can be up to ~40 MHz; XCLK must divide 80 MHz, e.g. 20 MHz).
- **Wi-Fi** is often the cap: on-chip TCP throughput is on the order of **~20 MB/s** in ideal tests — far above what XGA JPEGs need on a typical LAN.
- Published example: **720p ~20 fps** over RTSP on ESP32-class hardware; **XGA + Wi-Fi ingest** on LAN is often **mid‑teens to ~25+ fps** depending on JPEG quality and Wi-Fi.

Bottleneck order here: **sensor JPEG size → ingest WebSocket → viewer `/watch` WebSocket**. If serial shows good fps but the browser lags, check Wi-Fi and server load; if `FB-OVF` appears, raise `kJpegBufferBytes` or lower quality / XCLK.

**OV3660 on XIAO:** prefer **XGA (1024×768)** over SVGA for stable JPEG capture ([esp32-camera #857](https://github.com/espressif/esp32-camera/issues/857)). Set **`CAM_ANTIBANDING_HZ`** to **50** or **60** for **horizontal** mains flicker. **Vertical** stripes are usually heavy JPEG compression (raise quality: lower `kJpegQuality` in the sketch), disabled lens correction, or Wi-Fi noise on the camera bus — the sketch enables OV3660 lenc/denoise/BPC and uses a higher-quality JPEG setting for that. Serial should show `PID=0x3660`. The board can run hot under continuous capture; airflow helps.

If `cam_hal: FB-OVF` still appears: raise `kJpegBufferBytes`, confirm OPI PSRAM, ESP32 Arduino **3.3.x**, and `WiFi.setSleep(false)`.

### cloudflared (testing only)

Do **not** use Cloudflare Tunnel for production streaming; **streaming over cloudflared is not allowed** under typical Cloudflare terms and is a poor fit for continuous JPEG/WebSocket video anyway. Use **LAN** (or your own VPN/reverse proxy) for real use.

`cloudflared tunnel --url http://127.0.0.1:8080` is only mentioned for **quick local testing** of HTTPS/login behavior—not for the camera ingest path or sustained viewing.
