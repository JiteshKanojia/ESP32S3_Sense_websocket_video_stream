#include <WiFi.h>
#include <WebSocketsClient.h>
#include <esp_camera.h>
#include "config.h"
#include "camera_pins.h"

// 1024x768 — FRAMESIZE_XGA. On OV3660 this mode is more reliable than SVGA
// (see esp32-camera issue #857 for XIAO Sense + OV3660).
static const framesize_t kFrameSize = FRAMESIZE_XGA;
// S3 XCLK must divide 80 MHz (20 MHz is a common stable choice).
static const int kXclkHz = 20000000;
// XGA JPEGs are often 40–120 KiB; undersized slots cause cam_hal: FB-OVF.
static const size_t kJpegBufferBytes = 128 * 1024;
// OV3660/OV2640: lower number = higher quality, larger JPEG (typ. 10–20 for stream).
static const int kJpegQuality = 17;

WebSocketsClient webSocket;
bool cameraReady = false;
bool ingestConnected = false;
char apiKeyHeader[96];

bool initCamera() {
  camera_config_t config;
  config.ledc_channel = LEDC_CHANNEL_0;
  config.ledc_timer = LEDC_TIMER_0;
  config.pin_d0 = Y2_GPIO_NUM;
  config.pin_d1 = Y3_GPIO_NUM;
  config.pin_d2 = Y4_GPIO_NUM;
  config.pin_d3 = Y5_GPIO_NUM;
  config.pin_d4 = Y6_GPIO_NUM;
  config.pin_d5 = Y7_GPIO_NUM;
  config.pin_d6 = Y8_GPIO_NUM;
  config.pin_d7 = Y9_GPIO_NUM;
  config.pin_xclk = XCLK_GPIO_NUM;
  config.pin_pclk = PCLK_GPIO_NUM;
  config.pin_vsync = VSYNC_GPIO_NUM;
  config.pin_href = HREF_GPIO_NUM;
  config.pin_sccb_sda = SIOD_GPIO_NUM;
  config.pin_sccb_scl = SIOC_GPIO_NUM;
  config.pin_pwdn = PWDN_GPIO_NUM;
  config.pin_reset = RESET_GPIO_NUM;
  config.xclk_freq_hz = kXclkHz;
  config.pixel_format = PIXFORMAT_JPEG;
  config.frame_size = kFrameSize;
  config.jpeg_quality = kJpegQuality;
  config.fb_count = 1;
  config.grab_mode = CAMERA_GRAB_WHEN_EMPTY;
  config.jpeg_buffer_size = kJpegBufferBytes;

  if (psramFound()) {
    config.fb_location = CAMERA_FB_IN_PSRAM;
    Serial.printf("camera: PSRAM free=%u\n", (unsigned)ESP.getFreePsram());
  } else {
    Serial.println("camera: enable OPI PSRAM in Arduino Tools");
    config.fb_location = CAMERA_FB_IN_DRAM;
    config.jpeg_buffer_size = 64 * 1024;
  }

  if (esp_camera_init(&config) != ESP_OK) {
    Serial.printf("camera init failed, psram=%u\n", (unsigned)ESP.getFreePsram());
    return false;
  }

  sensor_t *sensor = esp_camera_sensor_get();
  if (sensor) {
    sensor->set_quality(sensor, kJpegQuality);
    Serial.printf(
        "camera sensor PID=0x%04x (OV3660=0x3660)\n",
        (unsigned)sensor->id.PID);
  }

  cameraReady = true;
  Serial.println("camera ready 1024x768 JPEG");
  return true;
}

void connectWiFi() {
  WiFi.mode(WIFI_STA);
  WiFi.setSleep(false);
  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
  Serial.print("wifi");
  uint32_t lastDot = millis();
  while (WiFi.status() != WL_CONNECTED) {
    if (millis() - lastDot >= 500) {
      Serial.print(".");
      lastDot = millis();
    }
    delay(10);
  }
  Serial.println();
  Serial.println(WiFi.localIP());
}

void webSocketEvent(WStype_t type, uint8_t *payload, size_t length) {
  switch (type) {
  case WStype_DISCONNECTED:
    ingestConnected = false;
    Serial.println("ingest disconnected");
    break;
  case WStype_CONNECTED:
    ingestConnected = true;
    Serial.println("ingest connected");
    break;
  default:
    break;
  }
}

void connectIngest() {
  snprintf(apiKeyHeader, sizeof(apiKeyHeader), "X-API-Key: %s", INGEST_API_KEY);
  webSocket.setExtraHeaders(apiKeyHeader);
#if CAM_USE_TLS
  webSocket.beginSSL(CAM_HOST, 443, CAM_PATH);
#else
  webSocket.begin(CAM_HOST, CAM_PORT, CAM_PATH);
#endif
  webSocket.onEvent(webSocketEvent);
  webSocket.setReconnectInterval(2000);
}

void setup() {
  Serial.begin(115200);
  delay(500);

  if (!initCamera()) {
    Serial.println("camera init failed; check OPI PSRAM in Tools");
  }
  connectWiFi();
  connectIngest();

  camera_fb_t *warm = esp_camera_fb_get();
  if (warm) {
    esp_camera_fb_return(warm);
  }

#if CAM_USE_TLS
  Serial.printf("wss://%s%s\n", CAM_HOST, CAM_PATH);
#else
  Serial.printf("ws://%s:%d%s\n", CAM_HOST, CAM_PORT, CAM_PATH);
#endif
}

// OV3660 on XIAO can rarely pack multiple JPEGs in one fb; stream the last one.
static void jpegView(const uint8_t *buf, size_t len, const uint8_t **out, size_t *outLen) {
  *out = buf;
  *outLen = len;
  if (len < 4) {
    return;
  }
  size_t lastSOI = 0;
  bool multi = false;
  for (size_t i = 0; i + 1 < len; i++) {
    if (buf[i] == 0xFF && buf[i + 1] == 0xD8) {
      if (i != lastSOI && i > 0) {
        multi = true;
      }
      lastSOI = i;
    }
  }
  if (multi && lastSOI > 0) {
    *out = buf + lastSOI;
    *outLen = len - lastSOI;
  }
}

void loop() {
  webSocket.loop();

  if (WiFi.status() != WL_CONNECTED) {
    WiFi.reconnect();
    delay(200);
    return;
  }
  if (!cameraReady || !ingestConnected) {
    return;
  }

  camera_fb_t *fb = esp_camera_fb_get();
  if (!fb) {
    return;
  }
  if (fb->format != PIXFORMAT_JPEG || fb->len == 0) {
    esp_camera_fb_return(fb);
    return;
  }

  const uint8_t *jpeg = fb->buf;
  size_t frameLen = fb->len;
  jpegView(fb->buf, fb->len, &jpeg, &frameLen);
  bool sent = webSocket.sendBIN(jpeg, frameLen);
  esp_camera_fb_return(fb);
  if (!sent) {
    return;
  }

  static uint32_t lastLog = 0;
  static uint32_t framesSinceLog = 0;
  static size_t lastBytes = 0;
  framesSinceLog++;
  lastBytes = frameLen;
  if (millis() - lastLog >= 2000) {
    float fps = framesSinceLog * 1000.0f / (millis() - lastLog);
    Serial.printf("%.1f fps, %u KB/frame\n", fps, (unsigned)(lastBytes / 1024));
    lastLog = millis();
    framesSinceLog = 0;
  }
}
