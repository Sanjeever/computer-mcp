#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <ImageIO/ImageIO.h>
#include "native_darwin.h"

static NSMutableDictionary *windows;
static NSMutableArray *elements;
static unsigned long nextWindow = 1;

static NSDictionary *failure(NSString *code, NSString *detail) { return @{ @"code":code, @"detail":detail ?: @"" }; }
static id attr(AXUIElementRef e, CFStringRef name) {
    CFTypeRef v = NULL;
    if (AXUIElementCopyAttributeValue(e, name, &v) != kAXErrorSuccess) return nil;
    return [(id)v autorelease];
}
static NSString *text(id v) {
    if ([v isKindOfClass:NSString.class]) return [v substringToIndex:MIN([v length], 1024)];
    if ([v isKindOfClass:NSNumber.class]) return [v stringValue];
    return @"";
}
static CGRect bounds(AXUIElementRef e) {
    CGPoint p = CGPointZero; CGSize s = CGSizeZero;
    id pv=attr(e,kAXPositionAttribute), sv=attr(e,kAXSizeAttribute);
    if (pv && CFGetTypeID((CFTypeRef)pv)==AXValueGetTypeID()) AXValueGetValue((AXValueRef)pv,kAXValueCGPointType,&p);
    if (sv && CFGetTypeID((CFTypeRef)sv)==AXValueGetTypeID()) AXValueGetValue((AXValueRef)sv,kAXValueCGSizeType,&s);
    return (CGRect){p,s};
}
static NSDictionary *rect(CGRect r) { return @{ @"x":@(r.origin.x),@"y":@(r.origin.y),@"width":@(r.size.width),@"height":@(r.size.height) }; }
static AXUIElementRef windowRef(NSString *key) { return (AXUIElementRef)windows[key]; }
static NSDictionary *node(AXUIElementRef e, NSString *eid, NSString *parent) {
    NSArray *names=@[(id)kAXRoleAttribute,(id)kAXTitleAttribute,(id)kAXDescriptionAttribute,(id)kAXValueAttribute,(id)kAXEnabledAttribute,(id)kAXFocusedAttribute,(id)kAXSubroleAttribute];
    CFArrayRef values=NULL;
    AXError status=AXUIElementCopyMultipleAttributeValues(e,(CFArrayRef)names,0,&values);
    if (status!=kAXErrorSuccess || !values) return nil;
    NSArray *v=[(NSArray *)values autorelease];
    NSString *role=text(v[0]);
    if (role.length==0) return nil;
    BOOL password=[text(v[6]) isEqualToString:@"AXSecureTextField"];
    NSString *title=text(v[1]);if (!title.length) title=text(v[2]);
    BOOL enabled=![v[4] isKindOfClass:NSNumber.class] || [v[4] boolValue];
    BOOL focused=[v[5] isKindOfClass:NSNumber.class] && [v[5] boolValue];
    NSMutableDictionary *result=[@{@"element_id":eid,@"role":role,@"name":title,@"value":password?@"":text(v[3]),@"bounds":rect(bounds(e)),@"enabled":@(enabled),@"focused":@(focused),@"password":@(password)} mutableCopy];
    if (parent.length) result[@"parent_id"]=parent;
    return [result autorelease];
}
static NSDictionary *listWindows(void) {
    NSMutableDictionary *fresh=[NSMutableDictionary dictionary];
    NSMutableArray *out=[NSMutableArray array];
    pid_t front=NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier;
    for (NSRunningApplication *app in NSWorkspace.sharedWorkspace.runningApplications) {
        if (app.activationPolicy==NSApplicationActivationPolicyProhibited) continue;
        AXUIElementRef root=AXUIElementCreateApplication(app.processIdentifier);
        AXUIElementSetMessagingTimeout(root,0.15);
        id list=attr(root,kAXWindowsAttribute);
        id focused=attr(root,kAXFocusedWindowAttribute);
        if ([list isKindOfClass:NSArray.class]) for (id value in list) {
            AXUIElementRef w=(AXUIElementRef)value;
            if ([attr(w,kAXMinimizedAttribute) boolValue]) continue;
            CGRect r=bounds(w);if (r.size.width<=0 || r.size.height<=0) continue;
            NSString *key=nil;
            for (NSString *existing in windows) {if(CFEqual((CFTypeRef)windows[existing],w)){key=existing;break;}}
            if (!key) key=[NSString stringWithFormat:@"%d:%lu",app.processIdentifier,nextWindow++];
            fresh[key]=value;
            [out addObject:@{@"window_id":key,@"pid":@(app.processIdentifier),@"app":app.localizedName ?: @"",@"title":text(attr(w,kAXTitleAttribute)),@"bounds":rect(r),@"active":@(front==app.processIdentifier && focused && CFEqual((CFTypeRef)focused,w))}];
            if (out.count>=512) break;
        }
        CFRelease(root);
        if (out.count>=512) break;
    }
    [windows release];windows=[fresh mutableCopy];
    return @{@"windows":out};
}
static NSDictionary *observe(NSDictionary *args) {
    AXUIElementRef root=windowRef(args[@"window_id"]);
    if(!root)return failure(@"window_not_found",args[@"window_id"]);
    [elements release];elements=[NSMutableArray new];
    NSInteger limit=[args[@"limit"] integerValue];
    NSMutableArray *queue=[NSMutableArray arrayWithObject:@{@"node":(id)root,@"parent":@""}];
    NSMutableArray *out=[NSMutableArray array];
    NSMutableSet *visited=[NSMutableSet set];
    BOOL truncated=NO;NSUInteger cursor=0;
    CFAbsoluteTime deadline=CFAbsoluteTimeGetCurrent()+3.0;
    while(cursor<queue.count && out.count<(NSUInteger)limit && CFAbsoluteTimeGetCurrent()<deadline) {
        NSDictionary *entry=queue[cursor++];AXUIElementRef e=(AXUIElementRef)entry[@"node"];
        if([visited containsObject:(id)e])continue;[visited addObject:(id)e];
        AXUIElementSetMessagingTimeout(e,0.15);
        NSString *eid=[NSString stringWithFormat:@"%lu",(unsigned long)elements.count];
        NSDictionary *n=node(e,eid,entry[@"parent"]);
        if(!n){truncated=YES;continue;}
        [elements addObject:(id)e];[out addObject:n];
        CFIndex count=0;
        if(AXUIElementGetAttributeValueCount(e,kAXChildrenAttribute,&count)==kAXErrorSuccess && count>0) {
            CFArrayRef children=NULL;CFIndex take=MIN(count,MAX(0,limit-(NSInteger)queue.count));
            if(take<count)truncated=YES;
            if(take>0 && AXUIElementCopyAttributeValues(e,kAXChildrenAttribute,0,take,&children)==kAXErrorSuccess){
                for(id child in (NSArray *)children)[queue addObject:@{@"node":child,@"parent":eid}];
                CFRelease(children);
            }
        }
    }
    return @{@"elements":out,@"truncated":@(truncated || cursor<queue.count)};
}
static CGKeyCode keyCode(NSString *key) {
    NSDictionary *map=@{@"a":@0,@"s":@1,@"d":@2,@"f":@3,@"h":@4,@"g":@5,@"z":@6,@"x":@7,@"c":@8,@"v":@9,@"b":@11,@"q":@12,@"w":@13,@"e":@14,@"r":@15,@"y":@16,@"t":@17,@"1":@18,@"2":@19,@"3":@20,@"4":@21,@"6":@22,@"5":@23,@"9":@25,@"7":@26,@"8":@28,@"0":@29,@"o":@31,@"u":@32,@"i":@34,@"p":@35,@"enter":@36,@"l":@37,@"j":@38,@"k":@40,@"n":@45,@"m":@46,@"tab":@48,@"space":@49,@"backspace":@51,@"escape":@53,@"meta":@55,@"shift":@56,@"alt":@58,@"ctrl":@59,@"f1":@122,@"f2":@120,@"f3":@99,@"f4":@118,@"f5":@96,@"f6":@97,@"f7":@98,@"f8":@100,@"f9":@101,@"f10":@109,@"f11":@103,@"f12":@111,@"home":@115,@"end":@119,@"pageup":@116,@"pagedown":@121,@"delete":@117,@"left":@123,@"right":@124,@"down":@125,@"up":@126};
    return [map[key] unsignedShortValue];
}
static NSDictionary *dispatch(NSString *op,NSDictionary *args) {
    if([op isEqualToString:@"status"]){
        CGDirectDisplayID ids[32];uint32_t count=0;CGGetActiveDisplayList(32,ids,&count);
        NSMutableArray *displays=[NSMutableArray array];for(uint32_t i=0;i<count;i++)[displays addObject:@{@"display_id":[NSString stringWithFormat:@"%u",ids[i]],@"bounds":rect(CGDisplayBounds(ids[i]))}];
        return @{@"platform":@"darwin",@"accessibility":@((BOOL)AXIsProcessTrusted()),@"screen_capture":@((BOOL)CGPreflightScreenCaptureAccess()),@"displays":displays};
    }
    if(!AXIsProcessTrusted())return failure(@"permission_denied",@"Accessibility");
    if([op isEqualToString:@"windows"])return listWindows();
    if([op isEqualToString:@"observe"])return observe(args);
    if([op isEqualToString:@"resolve"]){
        NSUInteger i=[args[@"element_id"] integerValue];
        if(i>=elements.count)return failure(@"stale_snapshot",@"element");
        NSDictionary *n=node((AXUIElementRef)elements[i],args[@"element_id"],@"");return n ?: failure(@"stale_snapshot",@"element");
    }
    if([op isEqualToString:@"window"]){
        AXUIElementRef w=windowRef(args[@"window_id"]);if(!w)return failure(@"window_not_found",@"");
        AXError status;
        if([args[@"action"] isEqualToString:@"activate"]){
            pid_t pid=0;AXUIElementGetPid(w,&pid);
            if(![[NSRunningApplication runningApplicationWithProcessIdentifier:pid] activateWithOptions:0])return failure(@"native_error",@"activate application");
            status=AXUIElementPerformAction(w,kAXRaiseAction);
        }else{
            NSDictionary *r=args[@"bounds"];CGPoint p=CGPointMake([r[@"x"] doubleValue],[r[@"y"] doubleValue]);CGSize s=CGSizeMake([r[@"width"] doubleValue],[r[@"height"] doubleValue]);
            AXValueRef pv=AXValueCreate(kAXValueCGPointType,&p),sv=AXValueCreate(kAXValueCGSizeType,&s);
            status=AXUIElementSetAttributeValue(w,kAXPositionAttribute,pv);
            if(status==kAXErrorSuccess)status=AXUIElementSetAttributeValue(w,kAXSizeAttribute,sv);
            CFRelease(pv);CFRelease(sv);
        }
        if(status!=kAXErrorSuccess)return failure(@"native_error",[NSString stringWithFormat:@"AX error %d",status]);
        return @{};
    }
    if([op isEqualToString:@"mouse"]){
        CGPoint p=CGPointMake([args[@"x"] doubleValue],[args[@"y"] doubleValue]);NSString *button=args[@"button"],*phase=args[@"phase"];
        CGMouseButton b=[button isEqualToString:@"right"]?kCGMouseButtonRight:([button isEqualToString:@"middle"]?kCGMouseButtonCenter:kCGMouseButtonLeft);
        CGEventType type;
        if([phase isEqualToString:@"drag"])type=kCGEventLeftMouseDragged;
        else if([phase isEqualToString:@"down"])type=b==kCGMouseButtonLeft?kCGEventLeftMouseDown:(b==kCGMouseButtonRight?kCGEventRightMouseDown:kCGEventOtherMouseDown);
        else type=b==kCGMouseButtonLeft?kCGEventLeftMouseUp:(b==kCGMouseButtonRight?kCGEventRightMouseUp:kCGEventOtherMouseUp);
        CGEventRef event=CGEventCreateMouseEvent(NULL,type,p,b);if(!event)return failure(@"native_error",@"create mouse event");
        CGEventSetIntegerValueField(event,kCGMouseEventClickState,[args[@"count"] intValue]);CGEventPost(kCGHIDEventTap,event);CFRelease(event);return @{};
    }
    if([op isEqualToString:@"type"]){
        NSString *value=args[@"text"];NSUInteger length=value.length;UniChar *chars=malloc(sizeof(UniChar)*length);[value getCharacters:chars range:NSMakeRange(0,length)];
        for(int down=1;down>=0;down--){CGEventRef event=CGEventCreateKeyboardEvent(NULL,0,down);if(!event){free(chars);return failure(@"native_error",@"create keyboard event");}CGEventKeyboardSetUnicodeString(event,length,chars);CGEventPost(kCGHIDEventTap,event);CFRelease(event);}free(chars);return @{};
    }
    if([op isEqualToString:@"key"]){
        NSArray *keys=args[@"keys"];CGEventFlags flags=0;
        for(NSString *key in keys){if([key isEqualToString:@"meta"])flags|=kCGEventFlagMaskCommand;else if([key isEqualToString:@"ctrl"])flags|=kCGEventFlagMaskControl;else if([key isEqualToString:@"alt"])flags|=kCGEventFlagMaskAlternate;else if([key isEqualToString:@"shift"])flags|=kCGEventFlagMaskShift;}
        CGKeyCode code=keyCode(keys.lastObject);
        for(int down=1;down>=0;down--){CGEventRef event=CGEventCreateKeyboardEvent(NULL,code,down);if(!event)return failure(@"native_error",@"create keyboard event");CGEventSetFlags(event,flags);CGEventPost(kCGHIDEventTap,event);CFRelease(event);}return @{};
    }
    if([op isEqualToString:@"scroll"]){
        CGPoint p=CGPointMake([args[@"x"] doubleValue],[args[@"y"] doubleValue]);CGEventRef move=CGEventCreateMouseEvent(NULL,kCGEventMouseMoved,p,kCGMouseButtonLeft);if(!move)return failure(@"native_error",@"create mouse event");CGEventPost(kCGHIDEventTap,move);CFRelease(move);
        CGEventRef event=CGEventCreateScrollWheelEvent(NULL,kCGScrollEventUnitLine,2,-[args[@"dy"] intValue],-[args[@"dx"] intValue]);if(!event)return failure(@"native_error",@"create scroll event");CGEventPost(kCGHIDEventTap,event);CFRelease(event);return @{};
    }
    return failure(@"invalid_argument",op);
}
char *cm_call(const char *operation,const char *arguments){
    @autoreleasepool {
        if(!windows)windows=[NSMutableDictionary new];
        NSData *input=[[NSString stringWithUTF8String:arguments] dataUsingEncoding:NSUTF8StringEncoding];
        id args=[NSJSONSerialization JSONObjectWithData:input options:NSJSONReadingFragmentsAllowed error:NULL];
        NSDictionary *result=dispatch([NSString stringWithUTF8String:operation],args);
        NSData *json=[NSJSONSerialization dataWithJSONObject:result options:0 error:NULL];
        if(!json)return strdup("{\"code\":\"native_error\",\"detail\":\"JSON encoding failed\"}");
        char *out=malloc(json.length+1);memcpy(out,json.bytes,json.length);out[json.length]=0;return out;
    }
}
void cm_close(void){[windows release];windows=nil;[elements release];elements=nil;}

@interface CMCaptureResult : NSObject {
@public
    dispatch_semaphore_t semaphore;
    SCShareableContent *content;
    NSError *error;
    CGImageRef image;
}
@end
@implementation CMCaptureResult
- (instancetype)init { if((self=[super init]))semaphore=dispatch_semaphore_create(0);return self; }
- (void)dealloc { [content release];[error release];if(image)CGImageRelease(image);dispatch_release(semaphore);[super dealloc]; }
@end

unsigned char *cm_capture(double x,double y,double width,double height,int max_width,size_t *length,int *out_width,int *out_height,char **error){
    @autoreleasepool {
        if(!CGPreflightScreenCaptureAccess())return NULL;
        double scale=MIN(1.0,(double)max_width/width);scale=MIN(scale,2560.0/height);
        size_t w=MAX(1,(size_t)ceil(width*scale)),h=MAX(1,(size_t)ceil(height*scale));
        CGColorSpaceRef color=CGColorSpaceCreateDeviceRGB();CGContextRef canvas=CGBitmapContextCreate(NULL,w,h,8,w*4,color,(CGBitmapInfo)kCGImageAlphaPremultipliedLast);CGColorSpaceRelease(color);
        if(!canvas){*error=strdup("create capture buffer");return NULL;}
        CMCaptureResult *ready=[CMCaptureResult new];
        [SCShareableContent getShareableContentExcludingDesktopWindows:NO onScreenWindowsOnly:YES completionHandler:^(SCShareableContent *value,NSError *err){ready->content=[value retain];ready->error=[err retain];dispatch_semaphore_signal(ready->semaphore);}];
        if(dispatch_semaphore_wait(ready->semaphore,dispatch_time(DISPATCH_TIME_NOW,5*NSEC_PER_SEC))!=0){[ready release];CGContextRelease(canvas);*error=strdup("ScreenCaptureKit content timeout");return NULL;}
        if(ready->error||!ready->content){*error=strdup(ready->error.localizedDescription.UTF8String ?: "no screen content");[ready release];CGContextRelease(canvas);return NULL;}
        CGRect requested=CGRectMake(x,y,width,height);BOOL captured=NO;
        for(SCDisplay *display in ready->content.displays){
            CGRect frame=CGDisplayBounds(display.displayID),part=CGRectIntersection(requested,frame);if(CGRectIsNull(part)||CGRectIsEmpty(part))continue;
            SCContentFilter *filter=[[SCContentFilter alloc] initWithDisplay:display excludingWindows:@[]];
            SCStreamConfiguration *config=[SCStreamConfiguration new];config.sourceRect=CGRectMake(part.origin.x-frame.origin.x,part.origin.y-frame.origin.y,part.size.width,part.size.height);config.width=MAX(1,(size_t)ceil(part.size.width*scale));config.height=MAX(1,(size_t)ceil(part.size.height*scale));config.showsCursor=NO;
            CMCaptureResult *done=[CMCaptureResult new];
            [SCScreenshotManager captureImageWithFilter:filter configuration:config completionHandler:^(CGImageRef value,NSError *err){if(value)done->image=CGImageRetain(value);done->error=[err retain];dispatch_semaphore_signal(done->semaphore);}];
            [filter release];[config release];
            if(dispatch_semaphore_wait(done->semaphore,dispatch_time(DISPATCH_TIME_NOW,5*NSEC_PER_SEC))!=0){[done release];*error=strdup("ScreenCaptureKit image timeout");break;}
            if(!done->image){*error=strdup(done->error.localizedDescription.UTF8String ?: "screen capture failed");[done release];break;}
            CGRect dest=CGRectMake((part.origin.x-x)*scale,h-(part.origin.y-y+part.size.height)*scale,part.size.width*scale,part.size.height*scale);CGContextDrawImage(canvas,dest,done->image);[done release];captured=YES;
        }
        [ready release];
        if(*error || !captured){CGContextRelease(canvas);if(!*error)*error=strdup("region is outside active displays");return NULL;}
        CGImageRef img=CGBitmapContextCreateImage(canvas);CGContextRelease(canvas);
        NSMutableData *data=[NSMutableData data];CGImageDestinationRef destination=CGImageDestinationCreateWithData((CFMutableDataRef)data,CFSTR("public.jpeg"),1,NULL);
        CGImageDestinationAddImage(destination,img,(CFDictionaryRef)@{(id)kCGImageDestinationLossyCompressionQuality:@0.8});BOOL ok=CGImageDestinationFinalize(destination);CFRelease(destination);CGImageRelease(img);
        if(!ok){*error=strdup("JPEG encoding failed");return NULL;}
        unsigned char *out=malloc(data.length);memcpy(out,data.bytes,data.length);*length=data.length;*out_width=(int)w;*out_height=(int)h;return out;
    }
}
