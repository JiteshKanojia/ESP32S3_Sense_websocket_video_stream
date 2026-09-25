#include <WiFi.h>
#include <HTTPClient.h>
#include <WiFiClientSecure.h>
#include <esp_camera.h>
#include <esp_heap_caps.h>
#include "config.h"
#include "camera_pins.h"

// Target ~8–10 fps on LAN; raise if posts stay fast and no FB-OVF.
static const uint32_t FRAME_INTERVAL_MS = 100;

WiFiClient plainClient;
WiFiClientSecure tlsClient;
HTTPClient http;
bool cameraReady = false;
bool httpIngestOpen = false;
char ingestURL[128];

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
  config.xclk_freq_hz = 10000000;
  config.pixel_format = PIXFORMAT_JPEG;
  config.frame_size = FRAMESIZE_QVGA;
  config.jpeg_quality = 12;
  config.fb_count = 1;
  config.grab_mode = CAMERA_GRAB_WHEN_EMPTY;
  config.jpeg_buffer_size = 40 * 1024;

  if (psramFound()) {
    config.fb_location = CAMERA_FB_IN_PSRAM;
    Serial.printf("camera: PSRAM free=%u\n", (unsigned)ESP.getFreePsram());
  } else {
    Serial.println("camera: enable OPI PSRAM in Arduino Tools");
    config.fb_location = CAMERA_FB_IN_DRAM;
  }

  if (esp_camera_init(&config) != ESP_OK) {
    Serial.printf("camera init failed, psram=%u\n", (unsigned)ESP.getFreePsram());
    return false;
  }

  cameraReady = true;
  Serial.println("camera ready");
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

bool openIngestHttp() {
#if CAM_USE_TLS
  tlsClient.setInsecure();
  if (!http.begin(tlsClient, ingestURL)) {
    return false;
  }
#else
  if (!http.begin(plainClient, ingestURL)) {
    return false;
  }
#endif
  httpIngestOpen = true;
  return true;
}

bool postJPEG(uint8_t *data, size_t len) {
  if (!httpIngestOpen && !openIngestHttp()) {
    return false;
  }
  http.addHeader("Content-Type", "image/jpeg");
  http.addHeader("X-API-Key", INGEST_API_KEY);
  http.addHeader("Connection", "keep-alive");
  int code = http.POST(data, len);
  if (code == 204) {
    return true;
  }
#if !CAM_USE_TLS
  http.end();
  httpIngestOpen = false;
#endif
  return false;
}

void setup() {
  Serial.begin(115200);
  delay(500);
#if CAM_USE_TLS
  snprintf(ingestURL, sizeof(ingestURL), "https://%s%s", CAM_HOST, CAM_PATH);
#else
  snprintf(ingestURL, sizeof(ingestURL), "http://%s:%d%s", CAM_HOST, CAM_PORT, CAM_PATH);
#endif

  // XIAO ESP32S3: allocate camera DMA before Wi-Fi or init fails with "frame buffer malloc failed".
  if (!initCamera()) {
    Serial.println("camera init failed; check OPI PSRAM in Tools");
  }
  connectWiFi();
  openIngestHttp();
  Serial.println(ingestURL);
}

void loop() {
  if (WiFi.status() != WL_CONNECTED) {
    WiFi.reconnect();
    delay(200);
    return;
  }
  if (!cameraReady) {
    delay(1000);
    return;
  }

  static uint32_t lastPost = 0;
  static bool sensorWarmed = false;
  uint32_t now = millis();
  if ((int32_t)(now - lastPost) < (int32_t)FRAME_INTERVAL_MS) {
    delay(1);
    return;
  }

  if (!sensorWarmed) {
    camera_fb_t *stale = esp_camera_fb_get();
    if (stale) {
      esp_camera_fb_return(stale);
    }
    sensorWarmed = true;
  }

  camera_fb_t *fb = esp_camera_fb_get();
  if (!fb) {
    Serial.println("camera capture failed");
    delay(100);
    return;
  }
  if (fb->format != PIXFORMAT_JPEG || fb->len == 0) {
    esp_camera_fb_return(fb);
    delay(100);
    return;
  }

  size_t frameLen = fb->len;
  uint8_t *copy = (uint8_t *)heap_caps_malloc(frameLen, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (!copy) {
    copy = (uint8_t *)malloc(frameLen);
  }
  if (!copy) {
    esp_camera_fb_return(fb);
    Serial.println("no ram for frame copy");
    return;
  }
  memcpy(copy, fb->buf, frameLen);
  esp_camera_fb_return(fb);

  bool ok = postJPEG(copy, frameLen);
  heap_caps_free(copy);
  if (!ok) {
    Serial.println("post failed");
    delay(200);
    return;
  }

  lastPost = millis();

  static uint32_t lastLog = 0;
  static uint32_t framesSinceLog = 0;
  framesSinceLog++;
  if (millis() - lastLog >= 2000) {
    float fps = framesSinceLog * 1000.0f / (millis() - lastLog);
    Serial.printf("%.1f fps, last frame %u bytes\n", fps, (unsigned)frameLen);
    lastLog = millis();
    framesSinceLog = 0;
  }
}
