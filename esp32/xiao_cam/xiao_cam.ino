#include <WiFi.h>
#include <WebSocketsClient.h>
#include <esp_camera.h>
#include <esp_heap_caps.h>
#include "config.h"
#include "camera_pins.h"
#include "cam_log.h"

#ifndef CAM_AUDIO_ENABLE
#define CAM_AUDIO_ENABLE 1
#endif

#if CAM_AUDIO_ENABLE
#include "ESP_I2S.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#endif

#ifndef CAM_ANTIBANDING_HZ
#define CAM_ANTIBANDING_HZ 50
#endif

#ifndef CAM_WIFI_TX_POWER
#define CAM_WIFI_TX_POWER WIFI_POWER_19_5dBm
#endif

// XGA stream. Lower JPEG quality number = sharper/larger/slower; ~26 targets ~25+ fps on LAN.
static const framesize_t kFrameSize = FRAMESIZE_XGA;
static const int kXclkHz = 20000000;
static const size_t kJpegBufferBytes = 128 * 1024;
static const int kJpegQuality = 26;

static int runtimeQuality = kJpegQuality;
static int runtimeAntibanding = CAM_ANTIBANDING_HZ;
static int runtimeDenoise = 3;
static int runtimeLenc = 1;
static int runtimeSharpness = -2;
static int runtimeVflip = 0;
static int runtimeHmirror = 0;

#ifndef CAM_VFLIP
#define CAM_VFLIP 0
#endif
#ifndef CAM_HMIRROR
#define CAM_HMIRROR 0
#endif

static volatile bool settingsPending = false;
static char settingsJson[280];

WebSocketsClient webSocket;
bool cameraReady = false;
bool ingestConnected = false;

#if CAM_AUDIO_ENABLE
#ifndef CAM_AUDIO_CORE
#define CAM_AUDIO_CORE 0
#endif
static SemaphoreHandle_t wsMutex = nullptr;

static void wsLoopLocked() {
  if (!wsMutex) {
    webSocket.loop();
    return;
  }
  if (xSemaphoreTake(wsMutex, pdMS_TO_TICKS(3)) == pdTRUE) {
    webSocket.loop();
    xSemaphoreGive(wsMutex);
  }
}

// serviceWs: call webSocket.loop() inside lock (audio task). Loop() should pass false after wsLoopLocked().
static bool wsSendBinLocked(const uint8_t *data, size_t len, bool serviceWs) {
  if (!wsMutex) {
    return false;
  }
  if (xSemaphoreTake(wsMutex, pdMS_TO_TICKS(30)) != pdTRUE) {
    return false;
  }
  if (serviceWs) {
    webSocket.loop();
  }
  bool ok = webSocket.sendBIN(data, len);
  xSemaphoreGive(wsMutex);
  return ok;
}
#endif
char apiKeyHeader[96];
uint8_t *sendBuf = nullptr;
size_t sendBufCap = 0;

#if CAM_AUDIO_ENABLE
static const int kAudioSampleRate = 16000;
static const size_t kAudioPcmBytes = 640;
static const size_t kAudioHeaderBytes = 6;
static const size_t kAudioPacketBytes = kAudioHeaderBytes + kAudioPcmBytes;
// Seeed wiki: GPIO42 = PDM CLK, GPIO41 = PDM DATA → setPinsPdmRx(clk, data)
static const int kPdmClkPin = 42;
static const int kPdmDataPin = 41;

static I2SClass audioI2s;
static volatile bool audioReady = false;
#if CAM_LOOP_LOG
static uint32_t audioPacketsSent = 0;
#endif

static void packAudioFrame(uint8_t *out, const uint8_t *pcm, size_t pcmLen) {
  out[0] = 0xA1;
  out[1] = 0x01;
  out[2] = (uint8_t)(kAudioSampleRate & 0xFF);
  out[3] = (uint8_t)((kAudioSampleRate >> 8) & 0xFF);
  out[4] = (uint8_t)(pcmLen & 0xFF);
  out[5] = (uint8_t)((pcmLen >> 8) & 0xFF);
  memcpy(out + kAudioHeaderBytes, pcm, pcmLen);
}

static bool readPcmBlock(uint8_t *pcm, size_t bytes) {
  int16_t *samples = (int16_t *)pcm;
  const size_t needSamples = bytes / 2;
  size_t gotSamples = 0;
  uint32_t idle = 0;
  while (gotSamples < needSamples) {
    int sample = audioI2s.read();
    if (sample == -1 || sample == 1) {
      if (++idle > 2000) {
        return false;
      }
      vTaskDelay(1);
      continue;
    }
    idle = 0;
    samples[gotSamples++] = (int16_t)sample;
  }
  return gotSamples == needSamples;
}

// PDM capture + ingest send on CAM_AUDIO_CORE (Arduino loop/camera stays on the other core).
static void audioStreamTask(void *arg) {
  uint8_t pcm[kAudioPcmBytes];
  uint8_t packet[kAudioPacketBytes];
  for (;;) {
    if (!audioReady || !ingestConnected) {
      vTaskDelay(pdMS_TO_TICKS(10));
      continue;
    }
    if (!readPcmBlock(pcm, kAudioPcmBytes)) {
      vTaskDelay(1);
      continue;
    }
    packAudioFrame(packet, pcm, kAudioPcmBytes);
    if (wsSendBinLocked(packet, kAudioPacketBytes, true)) {
#if CAM_LOOP_LOG
      audioPacketsSent++;
#endif
    }
    taskYIELD();
  }
}

static bool initAudio() {
  if (!wsMutex) {
    wsMutex = xSemaphoreCreateMutex();
    if (!wsMutex) {
      CAM_LOGE("audio: ws mutex alloc failed");
      return false;
    }
  }
  audioI2s.setPinsPdmRx(kPdmClkPin, kPdmDataPin);
  if (!audioI2s.begin(I2S_MODE_PDM_RX, kAudioSampleRate, I2S_DATA_BIT_WIDTH_16BIT, I2S_SLOT_MODE_MONO)) {
    CAM_LOGE("audio: PDM init failed (Sense hat? pins 42=CLK 41=DATA)");
    return false;
  }
  delay(50);
  if (xTaskCreatePinnedToCore(audioStreamTask, "audio", 6144, nullptr, 2, nullptr, CAM_AUDIO_CORE) != pdPASS) {
    CAM_LOGE("audio: task create failed");
    return false;
  }
  audioReady = true;
  CAM_LOGI("audio: PDM %d Hz on core %d (camera on other core)", kAudioSampleRate, CAM_AUDIO_CORE);
  return true;
}
#endif

// OV3660 anti-flicker (50/60 Hz mains). AEC can overwrite band registers — re-apply in loop.
static void applyAntibanding(sensor_t *sensor, int hz, bool log) {
  if (!sensor || !sensor->set_reg || hz == 0) {
    return;
  }
  if (sensor->id.PID != OV3660_PID) {
    if (log) {
      CAM_LOGD("antibanding: skipped (not OV3660)");
    }
    return;
  }

  if (sensor->set_aec2) {
    sensor->set_aec2(sensor, 0);
  }
  if (sensor->set_exposure_ctrl) {
    sensor->set_exposure_ctrl(sensor, 1);
  }
  if (sensor->set_gain_ctrl) {
    sensor->set_gain_ctrl(sensor, 1);
  }

  sensor->set_reg(sensor, 0x3c00, 0xff, 0x04);
  if (hz == 50) {
    sensor->set_reg(sensor, 0x3c01, 0xff, 0x81);
    sensor->set_reg(sensor, 0x3a08, 0xff, 0x00);
    sensor->set_reg(sensor, 0x3a09, 0xff, 0x62);
    sensor->set_reg(sensor, 0x3a0e, 0xff, 0x0a);
    sensor->set_reg(sensor, 0x3a14, 0xff, 0x09);
    sensor->set_reg(sensor, 0x3a15, 0xff, 0x30);
    if (log) {
      CAM_LOGD("antibanding 50Hz");
    }
  } else if (hz == 60) {
    sensor->set_reg(sensor, 0x3c01, 0xff, 0x82);
    sensor->set_reg(sensor, 0x3a0a, 0xff, 0x00);
    sensor->set_reg(sensor, 0x3a0b, 0xff, 0x52);
    sensor->set_reg(sensor, 0x3a0d, 0xff, 0x09);
    if (log) {
      CAM_LOGD("antibanding 60Hz");
    }
  }

}

// Vertical banding is usually JPEG blocks, ISP off, or Wi-Fi EMI on DVP — not 50 Hz mains.
static void applyImageTuning(sensor_t *sensor) {
  if (!sensor || sensor->id.PID != OV3660_PID) {
    return;
  }
  if (sensor->set_lenc) {
    sensor->set_lenc(sensor, runtimeLenc);
  }
  if (sensor->set_denoise) {
    sensor->set_denoise(sensor, runtimeDenoise);
  }
  if (sensor->set_bpc) {
    sensor->set_bpc(sensor, 1);
  }
  if (sensor->set_wpc) {
    sensor->set_wpc(sensor, 1);
  }
  if (sensor->set_raw_gma) {
    sensor->set_raw_gma(sensor, 1);
  }
  if (sensor->set_sharpness) {
    sensor->set_sharpness(sensor, runtimeSharpness);
  }
}

static void applyOrientation(sensor_t *sensor) {
  if (!sensor) {
    return;
  }
  if (sensor->set_vflip) {
    sensor->set_vflip(sensor, runtimeVflip);
  }
  if (sensor->set_hmirror) {
    sensor->set_hmirror(sensor, runtimeHmirror);
  }
}

static bool jsonInt(const char *json, const char *key, int *out) {
  char needle[24];
  snprintf(needle, sizeof(needle), "\"%s\":", key);
  const char *p = strstr(json, needle);
  if (!p) {
    return false;
  }
  p += strlen(needle);
  while (*p == ' ') {
    p++;
  }
  *out = atoi(p);
  return true;
}

static void applySettingsFromJson(const char *json) {
  sensor_t *sensor = esp_camera_sensor_get();
  if (!sensor) {
    return;
  }
  int v;
  if (jsonInt(json, "quality", &v) && v >= 4 && v <= 63) {
    runtimeQuality = v;
    sensor->set_quality(sensor, v);
  }
  if (jsonInt(json, "antibanding", &v) && (v == 0 || v == 50 || v == 60)) {
    runtimeAntibanding = v;
    applyAntibanding(sensor, v, false);
  }
  if (jsonInt(json, "denoise", &v) && v >= 0 && v <= 8) {
    runtimeDenoise = v;
    if (sensor->set_denoise) {
      sensor->set_denoise(sensor, v);
    }
  }
  if (jsonInt(json, "lenc", &v) && (v == 0 || v == 1)) {
    runtimeLenc = v;
    if (sensor->set_lenc) {
      sensor->set_lenc(sensor, v);
    }
  }
  if (jsonInt(json, "sharpness", &v) && v >= -3 && v <= 3) {
    runtimeSharpness = v;
    if (sensor->set_sharpness) {
      sensor->set_sharpness(sensor, v);
    }
  }
  if (jsonInt(json, "brightness", &v) && v >= -2 && v <= 2 && sensor->set_brightness) {
    sensor->set_brightness(sensor, v);
  }
  if (jsonInt(json, "vflip", &v) && (v == 0 || v == 1)) {
    runtimeVflip = v;
  }
  if (jsonInt(json, "hmirror", &v) && (v == 0 || v == 1)) {
    runtimeHmirror = v;
  }
  applyOrientation(sensor);
  CAM_LOG_LOOP_D("camera settings applied");
}

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
  config.fb_count = 2;
  config.grab_mode = CAMERA_GRAB_LATEST;
  config.jpeg_buffer_size = kJpegBufferBytes;

  if (psramFound()) {
    config.fb_location = CAMERA_FB_IN_PSRAM;
    CAM_LOGI("camera: PSRAM free=%u", (unsigned)ESP.getFreePsram());
  } else {
    CAM_LOGW("camera: enable OPI PSRAM in Arduino Tools");
    config.fb_location = CAMERA_FB_IN_DRAM;
    config.fb_count = 1;
    config.jpeg_buffer_size = 64 * 1024;
  }

  if (esp_camera_init(&config) != ESP_OK) {
    CAM_LOGE("camera init failed, psram=%u", (unsigned)ESP.getFreePsram());
    return false;
  }

  runtimeVflip = CAM_VFLIP ? 1 : 0;
  runtimeHmirror = CAM_HMIRROR ? 1 : 0;

  sensor_t *sensor = esp_camera_sensor_get();
  if (sensor) {
    sensor->set_quality(sensor, kJpegQuality);
    CAM_LOGI("camera sensor PID=0x%04x (OV3660=0x3660)", (unsigned)sensor->id.PID);
    applyImageTuning(sensor);
    applyOrientation(sensor);
    applyAntibanding(sensor, runtimeAntibanding, false);
  }

  sendBufCap = kJpegBufferBytes;
  sendBuf = (uint8_t *)heap_caps_malloc(sendBufCap, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  if (!sendBuf) {
    sendBuf = (uint8_t *)malloc(sendBufCap);
  }
  if (!sendBuf) {
    CAM_LOGE("send buffer alloc failed");
    return false;
  }

  cameraReady = true;
  CAM_LOGI("camera ready 1024x768");
  return true;
}

void connectWiFi() {
  WiFi.mode(WIFI_STA);
  WiFi.setSleep(false);
  WiFi.setAutoReconnect(true);
  WiFi.setTxPower(CAM_WIFI_TX_POWER);
  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
  while (WiFi.status() != WL_CONNECTED) {
    delay(10);
  }
  CAM_LOGI("wifi %s", WiFi.localIP().toString().c_str());
}

void webSocketEvent(WStype_t type, uint8_t *payload, size_t length) {
  switch (type) {
  case WStype_DISCONNECTED:
    ingestConnected = false;
    CAM_LOGI("ingest disconnected");
    break;
  case WStype_CONNECTED:
    ingestConnected = true;
    CAM_LOGI("ingest connected");
    break;
  case WStype_TEXT:
    if (length >= sizeof(settingsJson)) {
      length = sizeof(settingsJson) - 1;
    }
    memcpy(settingsJson, payload, length);
    settingsJson[length] = '\0';
    settingsPending = true;
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
  camLogInit(115200);
  delay(500);

  if (!initCamera()) {
    CAM_LOGE("camera init failed; check OPI PSRAM in Tools");
  }
  connectWiFi();
#if CAM_AUDIO_ENABLE
  if (!initAudio()) {
    CAM_LOGW("audio disabled (init failed)");
  }
#endif
  connectIngest();

  camera_fb_t *warm = esp_camera_fb_get();
  if (warm) {
    esp_camera_fb_return(warm);
  }

#if CAM_USE_TLS
  CAM_LOGI("wss://%s%s", CAM_HOST, CAM_PATH);
#else
  CAM_LOGI("ws://%s:%d%s", CAM_HOST, CAM_PORT, CAM_PATH);
#endif
}

// OV3660 can emit multiple JPEG SOIs in one fb; keep the last scan (rare path).
static void jpegView(const uint8_t *buf, size_t len, const uint8_t **out, size_t *outLen) {
  *out = buf;
  *outLen = len;
  if (len < 4 || buf[0] != 0xFF || buf[1] != 0xD8) {
    return;
  }
  size_t lastSOI = 0;
  for (size_t i = 0; i + 1 < len; i++) {
    if (buf[i] == 0xFF && buf[i + 1] == 0xD8) {
      lastSOI = i;
    }
  }
  if (lastSOI > 0) {
    *out = buf + lastSOI;
    *outLen = len - lastSOI;
  }
}

void loop() {
#if CAM_AUDIO_ENABLE
  wsLoopLocked();
#else
  webSocket.loop();
#endif

  if (WiFi.status() != WL_CONNECTED) {
    WiFi.reconnect();
    delay(200);
    return;
  }

  if (!cameraReady || !ingestConnected || !sendBuf) {
    return;
  }

  if (settingsPending) {
    settingsPending = false;
    applySettingsFromJson(settingsJson);
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
  if (frameLen > sendBufCap) {
    esp_camera_fb_return(fb);
    CAM_LOG_LOOP_W("frame larger than send buffer");
    return;
  }
  memcpy(sendBuf, jpeg, frameLen);
  esp_camera_fb_return(fb);

#if CAM_AUDIO_ENABLE
  if (!wsSendBinLocked(sendBuf, frameLen, false)) {
    return;
  }
#else
  if (!webSocket.sendBIN(sendBuf, frameLen)) {
    return;
  }
#endif

#if CAM_LOOP_LOG
  static uint32_t statsSinceMs = 0;
  static uint32_t framesSinceStats = 0;
  static size_t lastBytes = 0;
  framesSinceStats++;
  lastBytes = frameLen;
  uint32_t elapsed = millis() - statsSinceMs;
  if (statsSinceMs == 0) {
    statsSinceMs = millis();
  }
  if (elapsed >= (uint32_t)CAM_STATS_INTERVAL_MS) {
    float fps = framesSinceStats * 1000.0f / (float)elapsed;
#if CAM_AUDIO_ENABLE
    int audioPs = (int)(audioPacketsSent * 1000 / (elapsed ? elapsed : 1));
    audioPacketsSent = 0;
#else
    int audioPs = -1;
#endif
    camLogStreamStats(fps, lastBytes / 1024, runtimeQuality, audioPs);
    framesSinceStats = 0;
    statsSinceMs = millis();
  }
#endif
}
