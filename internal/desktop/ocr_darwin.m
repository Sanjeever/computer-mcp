#include "native_darwin.h"
#import <Cocoa/Cocoa.h>
#import <ImageIO/ImageIO.h>
#import <Vision/Vision.h>

char *cm_recognize(const unsigned char *bytes, size_t length, int limit) {
    @autoreleasepool {
        NSData *data = [NSData dataWithBytesNoCopy:(void *)bytes length:length freeWhenDone:NO];
        CGImageSourceRef source = CGImageSourceCreateWithData((CFDataRef)data, NULL);
        CGImageRef image = source ? CGImageSourceCreateImageAtIndex(source, 0, NULL) : NULL;
        if (source) CFRelease(source);
        if (!image) return strdup("{\"code\":\"native_error\",\"detail\":\"cannot decode OCR image\"}");
        VNImageRequestHandler *handler = [[VNImageRequestHandler alloc] initWithCGImage:image options:@{}];
        VNRecognizeTextRequest *request = [VNRecognizeTextRequest new];
        request.recognitionLevel = VNRequestTextRecognitionLevelAccurate;
        request.usesLanguageCorrection = NO;
        request.automaticallyDetectsLanguage = YES;
        request.recognitionLanguages = @[@"en-US", @"zh-Hans"];
        NSError *error = nil;
        NSDictionary *result;
        if (![handler performRequests:@[request] error:&error]) {
            result = @{@"code": @"native_error", @"detail": error.localizedDescription ?: @"Vision recognition failed"};
        } else {
            NSMutableArray *regions = [NSMutableArray array];
            double width = CGImageGetWidth(image), height = CGImageGetHeight(image);
            for (VNRecognizedTextObservation *observation in request.results) {
                if (regions.count >= (NSUInteger)limit) break;
                VNRecognizedText *candidate = [observation topCandidates:1].firstObject;
                if (!candidate.string.length) continue;
                CGRect box = observation.boundingBox;
                NSString *text = [candidate.string substringToIndex:MIN(candidate.string.length, 1024)];
                [regions addObject:@{@"text": text, @"confidence": @(candidate.confidence),
                    @"bounds": @{@"x": @(box.origin.x * width), @"y": @((1 - CGRectGetMaxY(box)) * height),
                                 @"width": @(box.size.width * width), @"height": @(box.size.height * height)}}];
            }
            result = @{@"regions": regions, @"truncated": @((BOOL)(request.results.count > (NSUInteger)limit))};
        }
        NSData *json = [NSJSONSerialization dataWithJSONObject:result options:0 error:NULL];
        char *output = json ? strndup(json.bytes, json.length) : NULL;
        [request release];
        [handler release];
        CGImageRelease(image);
        return output;
    }
}
