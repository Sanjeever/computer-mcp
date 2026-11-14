#include <stddef.h>
char *cm_call(const char *operation, const char *arguments);
unsigned char *cm_capture(const char *window_id, double x, double y, double width, double height, int max_width,
                          size_t *length, int *out_width, int *out_height, char **error);
void cm_close(void);
