#include <stddef.h>
#include <stdint.h>
enum {
    CM_CAPTURE_CONTENT = 1,
    CM_CAPTURE_IMAGE,
    CM_CAPTURE_ENCODE
};
void cm_capture_progress(uintptr_t handle, int stage);
char *cm_call(const char *operation, const char *arguments);
unsigned char *cm_capture(const char *window_id, double x, double y, double width, double height, int max_width,
                          uintptr_t progress, size_t *length, int *out_width, int *out_height, char **error);
void cm_close(void);
char *cm_recognize(const unsigned char *data, size_t length, int limit);
