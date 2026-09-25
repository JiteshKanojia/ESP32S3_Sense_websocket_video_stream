#pragma once

#include <Arduino.h>
#include <esp_log.h>
#include <stdarg.h>

#ifndef CAM_LOG_TAG
#define CAM_LOG_TAG "xiao_cam"
#endif

// ESP_LOG_NONE .. ESP_LOG_VERBOSE (see esp_log.h). INFO = boot + link events.
#ifndef CAM_LOG_LEVEL
#define CAM_LOG_LEVEL ESP_LOG_INFO
#endif

// 1 = fps stats + any CAM_LOG_LOOP_* from loop(); 0 = none (best for streaming audio).
#ifndef CAM_LOOP_LOG
#define CAM_LOOP_LOG 0
#endif

// Periodic fps line when CAM_LOOP_LOG is 1.
#ifndef CAM_STATS_INTERVAL_MS
#define CAM_STATS_INTERVAL_MS 2000
#endif

#if CAM_LOOP_LOG
#define CAM_LOG_LOOP_I(...) CAM_LOGI(__VA_ARGS__)
#define CAM_LOG_LOOP_W(...) CAM_LOGW(__VA_ARGS__)
#define CAM_LOG_LOOP_D(...) CAM_LOGD(__VA_ARGS__)
#else
#define CAM_LOG_LOOP_I(...) ((void)0)
#define CAM_LOG_LOOP_W(...) ((void)0)
#define CAM_LOG_LOOP_D(...) ((void)0)
#endif

inline void camLogInit(uint32_t baud = 115200) {
  Serial.begin(baud);
  esp_log_level_set(CAM_LOG_TAG, CAM_LOG_LEVEL);
  esp_log_level_set("cam_hal", ESP_LOG_WARN);
}

inline void camLogWrite(esp_log_level_t level, const char *fmt, ...) {
  if (level > CAM_LOG_LEVEL) {
    return;
  }
  char buf[160];
  va_list args;
  va_start(args, fmt);
  int n = vsnprintf(buf, sizeof(buf), fmt, args);
  va_end(args);
  if (n < 0) {
    return;
  }
  if ((size_t)n >= sizeof(buf)) {
    n = (int)sizeof(buf) - 1;
    buf[n] = '\0';
  }
  esp_log_write(level, CAM_LOG_TAG, "%s\n", buf);
}

#if CAM_LOG_LEVEL >= ESP_LOG_ERROR
#define CAM_LOGE(...) camLogWrite(ESP_LOG_ERROR, __VA_ARGS__)
#else
#define CAM_LOGE(...) ((void)0)
#endif
#if CAM_LOG_LEVEL >= ESP_LOG_WARN
#define CAM_LOGW(...) camLogWrite(ESP_LOG_WARN, __VA_ARGS__)
#else
#define CAM_LOGW(...) ((void)0)
#endif
#if CAM_LOG_LEVEL >= ESP_LOG_INFO
#define CAM_LOGI(...) camLogWrite(ESP_LOG_INFO, __VA_ARGS__)
#else
#define CAM_LOGI(...) ((void)0)
#endif
#if CAM_LOG_LEVEL >= ESP_LOG_DEBUG
#define CAM_LOGD(...) camLogWrite(ESP_LOG_DEBUG, __VA_ARGS__)
#else
#define CAM_LOGD(...) ((void)0)
#endif

// Stream stats line (call from loop when your own interval elapses). audioPerSec < 0 omits audio.
inline void camLogStreamStats(float fps, size_t lastFrameKb, int quality, int audioPerSec) {
#if !CAM_LOOP_LOG || CAM_STATS_INTERVAL_MS <= 0
  (void)fps;
  (void)lastFrameKb;
  (void)quality;
  (void)audioPerSec;
  return;
#else
  if (audioPerSec >= 0) {
    CAM_LOGI("%.1f fps, %u KB (q=%d) aud~%d/s", fps, (unsigned)lastFrameKb, quality, audioPerSec);
  } else {
    CAM_LOGI("%.1f fps, %u KB (q=%d)", fps, (unsigned)lastFrameKb, quality);
  }
#endif
}
