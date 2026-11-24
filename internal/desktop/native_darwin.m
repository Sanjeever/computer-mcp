#include "native_darwin.h"
#import <ApplicationServices/ApplicationServices.h>
#import <Cocoa/Cocoa.h>
#import <ImageIO/ImageIO.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <Vision/Vision.h>

static NSMutableDictionary *windows;
static NSMutableDictionary *windowNumbers;
static NSMutableDictionary *elements;
static unsigned long nextWindow = 1;
static unsigned long nextElement = 1;

static NSDictionary *failure(NSString *code, NSString *detail) {
    return @{@"code" : code, @"detail" : detail ?: @""};
}
static id attr(AXUIElementRef e, CFStringRef name) {
    CFTypeRef v = NULL;
    if (AXUIElementCopyAttributeValue(e, name, &v) != kAXErrorSuccess)
        return nil;
    return [(id)v autorelease];
}
static NSString *text(id v) {
    if ([v isKindOfClass:NSString.class])
        return [v substringToIndex:MIN([v length], 1024)];
    if ([v isKindOfClass:NSNumber.class])
        return [v stringValue];
    return @"";
}
static CGRect bounds(AXUIElementRef e) {
    CGPoint p = CGPointZero;
    CGSize s = CGSizeZero;
    id pv = attr(e, kAXPositionAttribute), sv = attr(e, kAXSizeAttribute);
    if (pv && CFGetTypeID((CFTypeRef)pv) == AXValueGetTypeID())
        AXValueGetValue((AXValueRef)pv, kAXValueCGPointType, &p);
    if (sv && CFGetTypeID((CFTypeRef)sv) == AXValueGetTypeID())
        AXValueGetValue((AXValueRef)sv, kAXValueCGSizeType, &s);
    return (CGRect){p, s};
}
static NSDictionary *rect(CGRect r) {
    return @{
        @"x" : @(r.origin.x),
        @"y" : @(r.origin.y),
        @"width" : @(r.size.width),
        @"height" : @(r.size.height)
    };
}
static AXUIElementRef windowRef(NSString *key) { return (AXUIElementRef)windows[key]; }
static NSString *blockedBy(AXUIElementRef w) {
    CFArrayRef children = NULL;
    CFIndex count = 0;
    if (AXUIElementGetAttributeValueCount(w, kAXChildrenAttribute, &count) == kAXErrorSuccess && count > 0 &&
        AXUIElementCopyAttributeValues(w, kAXChildrenAttribute, 0, MIN(count, 64), &children) == kAXErrorSuccess) {
        for (id child in (NSArray *)children) {
            if ([text(attr((AXUIElementRef)child, kAXRoleAttribute)) isEqualToString:(id)kAXSheetRole]) {
                NSString *title = text(attr((AXUIElementRef)child, kAXTitleAttribute));
                CFRelease(children);
                return title.length ? [@"sheet: " stringByAppendingString:title] : @"modal sheet";
            }
        }
        CFRelease(children);
    }
    return @"";
}
static NSNumber *windowNumber(AXUIElementRef w, pid_t pid, NSArray *screenWindows, BOOL *ambiguous) {
    CGRect r = bounds(w);
    NSString *title = text(attr(w, kAXTitleAttribute));
    NSMutableArray *matches = [NSMutableArray array];
    for (NSDictionary *info in screenWindows) {
        CGRect candidate;
        if ([info[(id)kCGWindowOwnerPID] intValue] != pid ||
            ![info[(id)kCGWindowIsOnscreen] boolValue] ||
            !CGRectMakeWithDictionaryRepresentation((CFDictionaryRef)info[(id)kCGWindowBounds], &candidate))
            continue;
        NSString *candidateTitle = info[(id)kCGWindowName];
        if (title.length && candidateTitle.length && ![candidateTitle isEqualToString:title])
            continue;
        if (fabs(r.origin.x - candidate.origin.x) <= 2 &&
            fabs(r.origin.y - candidate.origin.y) <= 2 &&
            fabs(r.size.width - candidate.size.width) <= 2 &&
            fabs(r.size.height - candidate.size.height) <= 2)
            [matches addObject:info];
    }
    *ambiguous = matches.count > 1;
    return matches.count == 1 ? matches[0][(id)kCGWindowNumber] : nil;
}
static NSString *rowText(AXUIElementRef e, int depth, int *budget) {
    if (depth == 0 || (*budget)-- <= 0)
        return @"";
    AXUIElementSetMessagingTimeout(e, 0.15);
    if ([text(attr(e, kAXSubroleAttribute)) isEqualToString:@"AXSecureTextField"])
        return @"";
    NSString *value = text(attr(e, kAXValueAttribute));
    if (!value.length)
        value = text(attr(e, kAXTitleAttribute));
    if (value.length)
        return value;
    CFIndex count = 0;
    CFArrayRef children = NULL;
    NSMutableArray *parts = [NSMutableArray array];
    if (AXUIElementGetAttributeValueCount(e, kAXChildrenAttribute, &count) == kAXErrorSuccess && count > 0 &&
        AXUIElementCopyAttributeValues(e, kAXChildrenAttribute, 0, MIN(count, 8), &children) == kAXErrorSuccess) {
        for (id child in (NSArray *)children) {
            NSString *part = rowText((AXUIElementRef)child, depth - 1, budget);
            if (part.length)
                [parts addObject:part];
            if (*budget <= 0)
                break;
        }
        CFRelease(children);
    }
    return text([parts componentsJoinedByString:@" · "]);
}
static NSDictionary *node(AXUIElementRef e, NSString *eid, NSString *parent) {
    NSArray *names = @[
        (id)kAXRoleAttribute, (id)kAXTitleAttribute, (id)kAXDescriptionAttribute,
        (id)kAXValueAttribute, (id)kAXEnabledAttribute, (id)kAXFocusedAttribute,
        (id)kAXSubroleAttribute
    ];
    CFArrayRef values = NULL;
    AXError status = AXUIElementCopyMultipleAttributeValues(e, (CFArrayRef)names, 0, &values);
    if (status != kAXErrorSuccess || !values)
        return nil;
    NSArray *v = [(NSArray *)values autorelease];
    NSString *role = text(v[0]);
    if (role.length == 0)
        return nil;
    BOOL password = [text(v[6]) isEqualToString:@"AXSecureTextField"];
    NSString *title = text(v[1]);
    if (!title.length)
        title = text(v[2]);
    if (!title.length && !password) {
        id label = attr(e, kAXTitleUIElementAttribute);
        if (label && ![text(attr((AXUIElementRef)label, kAXSubroleAttribute)) isEqualToString:@"AXSecureTextField"]) {
            title = text(attr((AXUIElementRef)label, kAXValueAttribute));
            if (!title.length)
                title = text(attr((AXUIElementRef)label, kAXTitleAttribute));
        }
        if (!title.length && [role isEqualToString:(id)kAXRowRole]) {
            int budget = 24;
            title = rowText(e, 4, &budget);
        }
    }
    BOOL enabled = ![v[4] isKindOfClass:NSNumber.class] || [v[4] boolValue];
    BOOL focused = [v[5] isKindOfClass:NSNumber.class] && [v[5] boolValue];
    NSMutableDictionary *result = [@{
        @"element_id" : eid,
        @"role" : role,
        @"name" : title,
        @"value" : password ? @"" : text(v[3]),
        @"bounds" : rect(bounds(e)),
        @"enabled" : @(enabled),
        @"focused" : @(focused),
        @"password" : @(password)
    } mutableCopy];
    if (parent.length)
        result[@"parent_id"] = parent;
    return [result autorelease];
}
static NSDictionary *listWindows(void) {
    NSMutableDictionary *fresh = [NSMutableDictionary dictionary];
    NSMutableDictionary *numbers = [NSMutableDictionary dictionary];
    NSMutableArray *out = [NSMutableArray array];
    pid_t front = 0;
    AXUIElementRef system = AXUIElementCreateSystemWide();
    id focusedApp = attr(system, kAXFocusedApplicationAttribute);
    if (focusedApp)
        AXUIElementGetPid((AXUIElementRef)focusedApp, &front);
    CFRelease(system);
    NSArray *screenWindows = [(NSArray *)CGWindowListCopyWindowInfo(
        kCGWindowListOptionAll | kCGWindowListExcludeDesktopElements, kCGNullWindowID)
        autorelease];
    NSMutableOrderedSet *pids = [NSMutableOrderedSet orderedSet];
    for (NSDictionary *info in screenWindows) {
        NSInteger layer = [info[(id)kCGWindowLayer] integerValue];
        if (layer >= NSNormalWindowLevel && layer < NSMainMenuWindowLevel &&
            [info[(id)kCGWindowIsOnscreen] boolValue])
            [pids addObject:info[(id)kCGWindowOwnerPID]];
    }
    for (NSNumber *process in pids) {
        pid_t pid = process.intValue;
        NSRunningApplication *app =
            [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
        if (!app || app.activationPolicy == NSApplicationActivationPolicyProhibited)
            continue;
        AXUIElementRef root = AXUIElementCreateApplication(pid);
        AXUIElementSetMessagingTimeout(root, 0.15);
        id list = attr(root, kAXWindowsAttribute);
        id focused = attr(root, kAXFocusedWindowAttribute);
        NSArray *currentWindows = [(NSArray *)CGWindowListCopyWindowInfo(
            kCGWindowListOptionAll | kCGWindowListExcludeDesktopElements, kCGNullWindowID) autorelease];
        if ([list isKindOfClass:NSArray.class])
            for (id value in list) {
                AXUIElementRef w = (AXUIElementRef)value;
                if ([attr(w, kAXMinimizedAttribute) boolValue])
                    continue;
                CGRect r = bounds(w);
                if (r.size.width <= 0 || r.size.height <= 0)
                    continue;
                NSString *key = nil;
                BOOL ambiguous = NO;
                NSNumber *number = windowNumber(w, pid, currentWindows, &ambiguous);
                for (NSString *existing in windows) {
                    pid_t previousPID = 0;
                    AXUIElementGetPid((AXUIElementRef)windows[existing], &previousPID);
                    if (previousPID == pid && (CFEqual((CFTypeRef)windows[existing], w) ||
                        (number && [number isEqual:windowNumbers[existing]]))) {
                        key = existing;
                        break;
                    }
                }
                if (!key && !number && !ambiguous)
                    continue;
                if (!key)
                    key = [NSString stringWithFormat:@"%d:%lu", pid, nextWindow++];
                if (!number && windowNumbers[key]) {
                    for (NSDictionary *info in screenWindows) {
                        if ([windowNumbers[key] isEqual:info[(id)kCGWindowNumber]] &&
                            [info[(id)kCGWindowIsOnscreen] boolValue] &&
                            pid == [info[(id)kCGWindowOwnerPID] intValue]) {
                            number = windowNumbers[key];
                            break;
                        }
                    }
                }
                fresh[key] = value;
                if (number)
                    numbers[key] = number;
                if (!number && !ambiguous) {
                    if (windowNumbers[key])
                        numbers[key] = windowNumbers[key];
                    continue;
                }
                NSString *blocker = blockedBy(w);
                BOOL keyWindow = focused && CFEqual((CFTypeRef)focused, w);
                BOOL panel = NO;
                if (front == pid && !keyWindow) {
                    NSString *subrole = text(attr(w, kAXSubroleAttribute));
                    panel = [subrole isEqualToString:(id)kAXFloatingWindowSubrole] ||
                            [subrole isEqualToString:(id)kAXSystemFloatingWindowSubrole];
                }
                if (!blocker.length && focused && !CFEqual((CFTypeRef)focused, w) &&
                    [attr((AXUIElementRef)focused, kAXModalAttribute) boolValue]) {
                    blocker = [@"modal window: " stringByAppendingString:text(attr((AXUIElementRef)focused, kAXTitleAttribute))];
                }
                [out addObject:@{
                    @"window_id" : key,
                    @"pid" : @(pid),
                    @"app" : app.localizedName ?: @"",
                    @"app_id" : app.bundleIdentifier ?: @"",
                    @"title" : text(attr(w, kAXTitleAttribute)),
                    @"bounds" : rect(r),
                    @"blocked_by" : blocker,
                    @"active" : @((BOOL)(front == pid && keyWindow)),
                    @"mouse_active" : @((BOOL)(front == pid && (keyWindow || panel)))
                }];
                if (out.count >= 512)
                    break;
            }
        CFRelease(root);
        if (out.count >= 512)
            break;
    }
    for (NSString *key in windows) {
        if (fresh[key] || fresh.count >= 1024)
            continue;
        NSNumber *number = windowNumbers[key];
        pid_t pid = 0;
        AXUIElementGetPid((AXUIElementRef)windows[key], &pid);
        BOOL live = NO;
        for (NSDictionary *info in screenWindows) {
            if (number && [number isEqual:info[(id)kCGWindowNumber]] &&
                pid == [info[(id)kCGWindowOwnerPID] intValue]) {
                live = YES;
                break;
            }
        }
        if (!live) {
            NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
            CFTypeRef role = NULL;
            AXUIElementSetMessagingTimeout((AXUIElementRef)windows[key], 0.15);
            AXError status = AXUIElementCopyAttributeValue((AXUIElementRef)windows[key], kAXRoleAttribute, &role);
            if (role) CFRelease(role);
            live = app && !app.terminated && (number || status != kAXErrorInvalidUIElement);
        }
        if (live) {
            fresh[key] = windows[key];
            if (number)
                numbers[key] = number;
        }
    }
    [windowNumbers release];
    windowNumbers = [numbers mutableCopy];
    [windows release];
    windows = [fresh mutableCopy];
    return @{@"windows" : out};
}
typedef struct {
    NSDictionary *options;
    NSMutableArray *out;
    NSMutableSet *visited;
    NSMapTable *previous;
    CFAbsoluteTime deadline;
    BOOL truncated;
} CMTraversal;

static BOOL traversalFull(CMTraversal *t) {
    return t->out.count >= [t->options[@"limit"] unsignedIntegerValue] ||
           t->visited.count >= 5000 || CFAbsoluteTimeGetCurrent() >= t->deadline;
}
static void visit(AXUIElementRef e, NSString *parent, NSInteger depth, CMTraversal *t) {
    if ([t->visited containsObject:(id)e])
        return;
    if (traversalFull(t)) {
        t->truncated = YES;
        return;
    }
    [t->visited addObject:(id)e];
    AXUIElementSetMessagingTimeout(e, 0.15);
    NSString *eid = [t->previous objectForKey:(id)e];
    if (!eid)
        eid = [NSString stringWithFormat:@"e%lu", nextElement++];
    NSDictionary *n = node(e, eid, parent);
    if (!n) {
        t->truncated = YES;
    } else {
        BOOL include = YES;
        NSDictionary *r = t->options[@"region"];
        if (r) {
            CGRect area = CGRectMake([r[@"x"] doubleValue], [r[@"y"] doubleValue],
                                     [r[@"width"] doubleValue], [r[@"height"] doubleValue]);
            include = CGRectIntersectsRect(area, bounds(e));
            if (!include && ([n[@"role"] isEqualToString:@"AXRow"] || [n[@"role"] isEqualToString:@"AXCell"]))
                return;
        }
        NSString *role = t->options[@"role"], *query = t->options[@"text"];
        if (role.length && [role caseInsensitiveCompare:n[@"role"]] != NSOrderedSame)
            include = NO;
        if (query.length && [n[@"name"] rangeOfString:query].location == NSNotFound &&
            [n[@"value"] rangeOfString:query].location == NSNotFound)
            include = NO;
        if ([t->options[@"compact"] boolValue] && ![n[@"name"] length] && ![n[@"value"] length] &&
            ![n[@"focused"] boolValue] &&
            [@[@"AXGroup", @"AXLayoutArea", @"AXScrollArea"] containsObject:n[@"role"]])
            include = NO;
        if (include) {
            elements[eid] = (id)e;
            [t->out addObject:n];
            parent = eid;
        }
    }
    CFIndex count = 0;
    AXError status = AXUIElementGetAttributeValueCount(e, kAXChildrenAttribute, &count);
    if (status == kAXErrorAttributeUnsupported || (status == kAXErrorSuccess && count == 0))
        return;
    if (status != kAXErrorSuccess || depth >= [t->options[@"max_depth"] integerValue]) {
        t->truncated = YES;
        return;
    }
    for (CFIndex offset = 0; offset < count; offset += 32) {
        if (traversalFull(t)) { t->truncated = YES; break; }
        CFArrayRef children = NULL;
        if (AXUIElementCopyAttributeValues(e, kAXChildrenAttribute, offset, MIN(32, count-offset), &children) != kAXErrorSuccess) {
            t->truncated = YES;
            break;
        }
        for (id child in (NSArray *)children) {
            if (traversalFull(t)) { t->truncated = YES; break; }
            visit((AXUIElementRef)child, parent, depth + 1, t);
        }
        CFRelease(children);
    }
}
static NSDictionary *observe(NSDictionary *args) {
    NSDictionary *options = args[@"options"];
    NSString *rootID = options[@"root_id"];
    AXUIElementRef root = rootID.length ? (AXUIElementRef)elements[rootID] : windowRef(args[@"window_id"]);
    if (!root)
        return failure(rootID.length ? @"stale_snapshot" : @"window_not_found", @"observation root");
    CFRetain(root);
    NSMapTable *previous = [NSMapTable strongToStrongObjectsMapTable];
    for (NSString *key in elements)
        [previous setObject:key forKey:elements[key]];
    [elements release];
    elements = [NSMutableDictionary new];
    CMTraversal t = {options, [NSMutableArray array], [NSMutableSet set], previous,
                     CFAbsoluteTimeGetCurrent() + 3.0, NO};
    visit(root, @"", 0, &t);
    CFRelease(root);
    return @{@"elements": t.out, @"truncated": @(t.truncated)};
}
static CGKeyCode keyCode(NSString *key) {
    NSDictionary *map = @{
        @"a" : @0,
        @"s" : @1,
        @"d" : @2,
        @"f" : @3,
        @"h" : @4,
        @"g" : @5,
        @"z" : @6,
        @"x" : @7,
        @"c" : @8,
        @"v" : @9,
        @"b" : @11,
        @"q" : @12,
        @"w" : @13,
        @"e" : @14,
        @"r" : @15,
        @"y" : @16,
        @"t" : @17,
        @"1" : @18,
        @"2" : @19,
        @"3" : @20,
        @"4" : @21,
        @"6" : @22,
        @"5" : @23,
        @"9" : @25,
        @"7" : @26,
        @"8" : @28,
        @"0" : @29,
        @"o" : @31,
        @"u" : @32,
        @"i" : @34,
        @"p" : @35,
        @"enter" : @36,
        @"l" : @37,
        @"j" : @38,
        @"k" : @40,
        @"n" : @45,
        @"m" : @46,
        @"tab" : @48,
        @"space" : @49,
        @"backspace" : @51,
        @"escape" : @53,
        @"meta" : @55,
        @"shift" : @56,
        @"alt" : @58,
        @"ctrl" : @59,
        @"f1" : @122,
        @"f2" : @120,
        @"f3" : @99,
        @"f4" : @118,
        @"f5" : @96,
        @"f6" : @97,
        @"f7" : @98,
        @"f8" : @100,
        @"f9" : @101,
        @"f10" : @109,
        @"f11" : @103,
        @"f12" : @111,
        @"home" : @115,
        @"end" : @119,
        @"pageup" : @116,
        @"pagedown" : @121,
        @"delete" : @117,
        @"left" : @123,
        @"right" : @124,
        @"down" : @125,
        @"up" : @126
    };
    return [map[key] unsignedShortValue];
}
static CGEventFlags keyFlags(NSArray *keys) {
    CGEventFlags flags = 0;
    if (![keys isKindOfClass:NSArray.class])
        return flags;
    for (NSString *key in keys) {
        if ([key isEqualToString:@"meta"])
            flags |= kCGEventFlagMaskCommand;
        else if ([key isEqualToString:@"ctrl"])
            flags |= kCGEventFlagMaskControl;
        else if ([key isEqualToString:@"alt"])
            flags |= kCGEventFlagMaskAlternate;
        else if ([key isEqualToString:@"shift"])
            flags |= kCGEventFlagMaskShift;
    }
    return flags;
}
static BOOL isMenuRole(NSString *role) {
    return [role isEqualToString:(id)kAXMenuRole] || [role isEqualToString:(id)kAXMenuItemRole];
}

static BOOL menuBelongsToWindow(AXUIElementRef item, AXUIElementRef window) {
    // A menu may have no AXWindow of its own. Require its AX parent chain to
    // reach the window or a control explicitly belonging to that window.
    for (int depth = 0; depth < 8; depth++) {
        AXUIElementRef parent = (AXUIElementRef)attr(item, kAXParentAttribute);
        if (!parent || CFGetTypeID(parent) != AXUIElementGetTypeID() || CFEqual(parent, item))
            return NO;
        AXUIElementSetMessagingTimeout(parent, 0.15);
        id owner = attr(parent, kAXWindowAttribute);
        if (CFEqual(parent, window) || (owner && CFEqual((CFTypeRef)owner, window)))
            return YES;
        if (!isMenuRole(text(attr(parent, kAXRoleAttribute))))
            return NO;
        item = parent;
    }
    return NO;
}

static NSDictionary *dispatch(NSString *op, NSDictionary *args) {
    if ([op isEqualToString:@"status"]) {
        CGDirectDisplayID ids[32];
        uint32_t count = 0;
        CGGetActiveDisplayList(32, ids, &count);
        NSMutableArray *displays = [NSMutableArray array];
        for (uint32_t i = 0; i < count; i++)
            [displays addObject:@{
                @"display_id" : [NSString stringWithFormat:@"%u", ids[i]],
                @"bounds" : rect(CGDisplayBounds(ids[i]))
            }];
        return @{
            @"platform" : @"darwin",
            @"accessibility" : @((BOOL)AXIsProcessTrusted()),
            @"screen_capture" : @((BOOL)CGPreflightScreenCaptureAccess()),
            @"ocr" : @YES,
            @"displays" : displays
        };
    }
    if (!AXIsProcessTrusted())
        return failure(@"permission_denied", @"Accessibility");
    if ([op isEqualToString:@"windows"])
        return listWindows();
    if ([op isEqualToString:@"observe"])
        return observe(args);
    if ([op isEqualToString:@"focus"]) {
        AXUIElementRef w = windowRef(args[@"window_id"]);
        if (!w)
            return failure(@"window_not_found", @"");
        pid_t pid = 0;
        AXUIElementGetPid(w, &pid);
        AXUIElementRef app = AXUIElementCreateApplication(pid);
        AXUIElementSetMessagingTimeout(app, 0.15);
        id focused = attr(app, kAXFocusedUIElementAttribute);
        CFRelease(app);
        if (!focused)
            return @{};
        id owner = attr((AXUIElementRef)focused, kAXWindowAttribute);
        if (!owner || !CFEqual((CFTypeRef)owner, w))
            return @{};
        return node((AXUIElementRef)focused, @"", @"") ?: @{};
    }
    if ([op isEqualToString:@"resolve"]) {
        AXUIElementRef e = (AXUIElementRef)elements[args[@"element_id"]];
        if (!e)
            return failure(@"stale_snapshot", @"element");
        NSDictionary *n = node(e, args[@"element_id"], @"");
        return n ?: failure(@"stale_snapshot", @"element");
    }
    if ([op isEqualToString:@"check_point"]) {
        AXUIElementRef w = windowRef(args[@"window_id"]);
        if (!w)
            return failure(@"window_not_found", @"");
        AXUIElementRef system = AXUIElementCreateSystemWide(), hit = NULL;
        AXUIElementSetMessagingTimeout(system, 0.15);
        AXError status = AXUIElementCopyElementAtPosition(system, [args[@"x"] floatValue],
                                                         [args[@"y"] floatValue], &hit);
        CFRelease(system);
        if (status != kAXErrorSuccess || !hit) {
            if (hit) CFRelease(hit);
            return failure(@"target_obscured", [NSString stringWithFormat:@"reason=unverified_point; ax_error=%d", status]);
        }
        AXUIElementSetMessagingTimeout(hit, 0.15);
        id owner = attr(hit, kAXWindowAttribute);
        BOOL matches = CFEqual(hit, w) || (owner && CFEqual((CFTypeRef)owner, w));
        NSDictionary *result = @{};
        if (!matches) {
            NSString *role = text(attr(hit, kAXRoleAttribute));
            pid_t targetPID = 0, hitPID = 0;
            AXUIElementGetPid(w, &targetPID);
            AXUIElementGetPid(hit, &hitPID);
            NSString *windowID = nil;
            for (NSString *key in windows) {
                if (CFEqual(hit, (CFTypeRef)windows[key]) || (owner && CFEqual((CFTypeRef)owner, windows[key]))) {
                    windowID = key;
                    break;
                }
            }
            NSString *reason = @"unverified_window";
            if (windowID || (hitPID > 0 && hitPID != targetPID)) {
                reason = @"other_window";
            } else if (hitPID > 0 && hitPID == targetPID && isMenuRole(role)) {
                BOOL owned = menuBelongsToWindow(hit, w);
                if ([args[@"allow_menu"] boolValue] && owned) {
                    CFRelease(hit);
                    return @{};
                }
                reason = owned ? @"menu_requires_screen_point" : @"unverified_menu";
            } else if (owner) {
                reason = @"other_window";
            }
            NSString *detail = [NSString stringWithFormat:@"reason=%@", reason];
            if (role.length)
                detail = [detail stringByAppendingFormat:@"; hit_role=%@", role];
            if (windowID)
                detail = [detail stringByAppendingFormat:@"; hit_window_id=%@", windowID];
            result = failure(@"target_obscured", detail);
        }
        CFRelease(hit);
        return result;
    }
    if ([op isEqualToString:@"window"]) {
        AXUIElementRef w = windowRef(args[@"window_id"]);
        if (!w)
            return failure(@"window_not_found", @"");
        AXError status;
        if ([args[@"action"] isEqualToString:@"activate"]) {
            NSString *blocker = blockedBy(w);
            if (blocker.length)
                return failure(@"window_blocked", blocker);
            pid_t pid = 0;
            AXUIElementGetPid(w, &pid);
            if (![[NSRunningApplication runningApplicationWithProcessIdentifier:pid]
                    activateWithOptions:0])
                return failure(@"native_error", @"activate application");
            status = AXUIElementPerformAction(w, kAXRaiseAction);
        } else {
            NSDictionary *r = args[@"bounds"];
            CGPoint p = CGPointMake([r[@"x"] doubleValue], [r[@"y"] doubleValue]);
            CGSize s = CGSizeMake([r[@"width"] doubleValue], [r[@"height"] doubleValue]);
            AXValueRef pv = AXValueCreate(kAXValueCGPointType, &p),
                       sv = AXValueCreate(kAXValueCGSizeType, &s);
            status = AXUIElementSetAttributeValue(w, kAXPositionAttribute, pv);
            if (status == kAXErrorSuccess)
                status = AXUIElementSetAttributeValue(w, kAXSizeAttribute, sv);
            CFRelease(pv);
            CFRelease(sv);
        }
        if (status != kAXErrorSuccess)
            return failure(@"native_error", [NSString stringWithFormat:@"AX error %d", status]);
        return @{};
    }
    if ([op isEqualToString:@"mouse"]) {
        CGPoint p = CGPointMake([args[@"x"] doubleValue], [args[@"y"] doubleValue]);
        NSString *button = args[@"button"], *phase = args[@"phase"];
        CGMouseButton b =
            [button isEqualToString:@"right"]
                ? kCGMouseButtonRight
                : ([button isEqualToString:@"middle"] ? kCGMouseButtonCenter : kCGMouseButtonLeft);
        CGEventType type;
        if ([phase isEqualToString:@"move"])
            type = kCGEventMouseMoved;
        else if ([phase isEqualToString:@"drag"])
            type = kCGEventLeftMouseDragged;
        else if ([phase isEqualToString:@"down"])
            type = b == kCGMouseButtonLeft ? kCGEventLeftMouseDown
                                           : (b == kCGMouseButtonRight ? kCGEventRightMouseDown
                                                                       : kCGEventOtherMouseDown);
        else
            type = b == kCGMouseButtonLeft
                       ? kCGEventLeftMouseUp
                       : (b == kCGMouseButtonRight ? kCGEventRightMouseUp : kCGEventOtherMouseUp);
        CGEventRef event = CGEventCreateMouseEvent(NULL, type, p, b);
        if (!event)
            return failure(@"native_error", @"create mouse event");
        CGEventSetIntegerValueField(event, kCGMouseEventClickState, [args[@"count"] intValue]);
        CGEventSetFlags(event, keyFlags(args[@"hold_keys"]));
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
        return @{};
    }
    if ([op isEqualToString:@"type"]) {
        NSString *value = args[@"text"];
        NSUInteger length = value.length;
        UniChar *chars = malloc(sizeof(UniChar) * length);
        [value getCharacters:chars range:NSMakeRange(0, length)];
        for (int down = 1; down >= 0; down--) {
            CGEventRef event = CGEventCreateKeyboardEvent(NULL, 0, down);
            if (!event) {
                free(chars);
                return failure(@"native_error", @"create keyboard event");
            }
            CGEventKeyboardSetUnicodeString(event, length, chars);
            CGEventSetFlags(event, 0);
            CGEventPost(kCGHIDEventTap, event);
            CFRelease(event);
        }
        free(chars);
        return @{};
    }
    if ([op isEqualToString:@"hold_keys"]) {
        NSArray *keys = args[@"keys"];
        BOOL down = [args[@"down"] boolValue], failed = NO;
        CGEventFlags flags = down ? 0 : keyFlags(keys);
        NSArray *ordered = down ? keys : keys.reverseObjectEnumerator.allObjects;
        for (NSString *key in ordered) {
            CGEventFlags flag = keyFlags(@[key]);
            flags = down ? flags | flag : flags & ~flag;
            CGEventRef event = CGEventCreateKeyboardEvent(NULL, keyCode(key), down);
            if (!event) {
                failed = YES;
                if (down) break;
                continue;
            }
            CGEventSetFlags(event, flags);
            CGEventPost(kCGHIDEventTap, event);
            CFRelease(event);
        }
        return failed ? failure(@"native_error", @"create held-key event") : @{};
    }
    if ([op isEqualToString:@"key"]) {
        NSArray *keys = args[@"keys"];
        CGEventFlags flags = keyFlags(keys);
        CGKeyCode code = keyCode(keys.lastObject);
        for (int down = 1; down >= 0; down--) {
            CGEventRef event = CGEventCreateKeyboardEvent(NULL, code, down);
            if (!event)
                return failure(@"native_error", @"create keyboard event");
            CGEventSetFlags(event, down ? flags : 0);
            CGEventPost(kCGHIDEventTap, event);
            CFRelease(event);
        }
        return @{};
    }
    if ([op isEqualToString:@"scroll"]) {
        CGPoint p = CGPointMake([args[@"x"] doubleValue], [args[@"y"] doubleValue]);
        CGEventRef move = CGEventCreateMouseEvent(NULL, kCGEventMouseMoved, p, kCGMouseButtonLeft);
        if (!move)
            return failure(@"native_error", @"create mouse event");
        CGEventSetFlags(move, keyFlags(args[@"hold_keys"]));
        CGEventPost(kCGHIDEventTap, move);
        CFRelease(move);
        CGEventRef event = CGEventCreateScrollWheelEvent(
            NULL, kCGScrollEventUnitLine, 2, -[args[@"dy"] intValue], -[args[@"dx"] intValue]);
        if (!event)
            return failure(@"native_error", @"create scroll event");
        CGEventSetFlags(event, keyFlags(args[@"hold_keys"]));
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
        return @{};
    }
    return failure(@"invalid_argument", op);
}
char *cm_call(const char *operation, const char *arguments) {
    @autoreleasepool {
        if (!windows)
            windows = [NSMutableDictionary new];
        NSData *input =
            [[NSString stringWithUTF8String:arguments] dataUsingEncoding:NSUTF8StringEncoding];
        id args = [NSJSONSerialization JSONObjectWithData:input
                                                  options:NSJSONReadingFragmentsAllowed
                                                    error:NULL];
        NSDictionary *result = dispatch([NSString stringWithUTF8String:operation], args);
        NSData *json = [NSJSONSerialization dataWithJSONObject:result options:0 error:NULL];
        if (!json)
            return strdup("{\"code\":\"native_error\",\"detail\":\"JSON encoding failed\"}");
        char *out = malloc(json.length + 1);
        memcpy(out, json.bytes, json.length);
        out[json.length] = 0;
        return out;
    }
}
void cm_close(void) {
    [windowNumbers release];
    windowNumbers = nil;
    [windows release];
    windows = nil;
    [elements release];
    elements = nil;
}

@interface CMCaptureResult : NSObject {
  @public
    dispatch_semaphore_t semaphore;
    SCShareableContent *content;
    NSError *error;
    CGImageRef image;
}
@end
@implementation CMCaptureResult
- (instancetype)init {
    if ((self = [super init]))
        semaphore = dispatch_semaphore_create(0);
    return self;
}
- (void)dealloc {
    [content release];
    [error release];
    if (image)
        CGImageRelease(image);
    dispatch_release(semaphore);
    [super dealloc];
}
@end

unsigned char *cm_capture(const char *window_id, double x, double y, double width, double height, int max_width,
                          size_t *length, int *out_width, int *out_height, char **error) {
    @autoreleasepool {
        if (!CGPreflightScreenCaptureAccess())
            return NULL;
        double scale = MIN(1.0, (double)max_width / width);
        scale = MIN(scale, 2560.0 / height);
        size_t w = MAX(1, (size_t)ceil(width * scale)), h = MAX(1, (size_t)ceil(height * scale));
        CGColorSpaceRef color = CGColorSpaceCreateDeviceRGB();
        CGContextRef canvas = CGBitmapContextCreate(NULL, w, h, 8, w * 4, color,
                                                    (CGBitmapInfo)kCGImageAlphaPremultipliedLast);
        CGColorSpaceRelease(color);
        if (!canvas) {
            *error = strdup("create capture buffer");
            return NULL;
        }
        CMCaptureResult *ready = [CMCaptureResult new];
        [SCShareableContent
            getShareableContentExcludingDesktopWindows:NO
                                   onScreenWindowsOnly:YES
                                     completionHandler:^(SCShareableContent *value, NSError *err) {
                                       ready->content = [value retain];
                                       ready->error = [err retain];
                                       dispatch_semaphore_signal(ready->semaphore);
                                     }];
        dispatch_semaphore_wait(ready->semaphore, DISPATCH_TIME_FOREVER);
        if (ready->error || !ready->content) {
            *error = strdup(ready->error.localizedDescription.UTF8String ?: "no screen content");
            [ready release];
            CGContextRelease(canvas);
            return NULL;
        }
        CGRect requested = CGRectMake(x, y, width, height);
        BOOL captured = NO;
        if (window_id) {
            NSString *key = [NSString stringWithUTF8String:window_id];
            NSNumber *number = windowNumbers[key];
            SCWindow *target = nil;
            for (SCWindow *candidate in ready->content.windows) {
                if (number && candidate.windowID == number.unsignedIntValue) { target = candidate; break; }
            }
            if (!target || !CGRectContainsRect(target.frame, requested)) {
                *error = strdup("window capture identity or geometry changed; observe again");
            } else if (target.frame.size.width * target.frame.size.height * scale * scale > 40000000) {
                *error = strdup("window capture exceeds 40 million source pixels");
            } else {
                SCContentFilter *filter = [[SCContentFilter alloc] initWithDesktopIndependentWindow:target];
                SCStreamConfiguration *config = [SCStreamConfiguration new];
                config.width = MAX(1, (size_t)ceil(target.frame.size.width * scale));
                config.height = MAX(1, (size_t)ceil(target.frame.size.height * scale));
                config.ignoreShadowsSingleWindow = YES;
                config.showsCursor = NO;
                CMCaptureResult *done = [CMCaptureResult new];
                [SCScreenshotManager captureImageWithFilter:filter configuration:config
                    completionHandler:^(CGImageRef value, NSError *err) {
                        if (value) done->image = CGImageRetain(value);
                        done->error = [err retain];
                        dispatch_semaphore_signal(done->semaphore);
                    }];
                [filter release];
                [config release];
                // A caller timeout must not permit overlapping native capture operations.
                dispatch_semaphore_wait(done->semaphore, DISPATCH_TIME_FOREVER);
                if (!done->image) {
                    *error = strdup(done->error.localizedDescription.UTF8String ?: "window capture failed");
                } else {
                    CGRect frame = target.frame;
                    CGRect dest = CGRectMake((frame.origin.x - x) * scale,
                        h - (frame.origin.y - y + frame.size.height) * scale,
                        frame.size.width * scale, frame.size.height * scale);
                    CGContextDrawImage(canvas, dest, done->image);
                    captured = YES;
                }
                [done release];
            }
        }
        for (SCDisplay *display in window_id ? @[] : ready->content.displays) {
            CGRect frame = CGDisplayBounds(display.displayID),
                   part = CGRectIntersection(requested, frame);
            if (CGRectIsNull(part) || CGRectIsEmpty(part))
                continue;
            SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:display
                                                              excludingWindows:@[]];
            SCStreamConfiguration *config = [SCStreamConfiguration new];
            config.sourceRect =
                CGRectMake(part.origin.x - frame.origin.x, part.origin.y - frame.origin.y,
                           part.size.width, part.size.height);
            config.width = MAX(1, (size_t)ceil(part.size.width * scale));
            config.height = MAX(1, (size_t)ceil(part.size.height * scale));
            config.showsCursor = NO;
            CMCaptureResult *done = [CMCaptureResult new];
            [SCScreenshotManager captureImageWithFilter:filter
                                          configuration:config
                                      completionHandler:^(CGImageRef value, NSError *err) {
                                        if (value)
                                            done->image = CGImageRetain(value);
                                        done->error = [err retain];
                                        dispatch_semaphore_signal(done->semaphore);
                                      }];
            [filter release];
            [config release];
            dispatch_semaphore_wait(done->semaphore, DISPATCH_TIME_FOREVER);
            if (!done->image) {
                *error =
                    strdup(done->error.localizedDescription.UTF8String ?: "screen capture failed");
                [done release];
                break;
            }
            CGRect dest = CGRectMake((part.origin.x - x) * scale,
                                     h - (part.origin.y - y + part.size.height) * scale,
                                     part.size.width * scale, part.size.height * scale);
            CGContextDrawImage(canvas, dest, done->image);
            [done release];
            captured = YES;
        }
        [ready release];
        if (*error || !captured) {
            CGContextRelease(canvas);
            if (!*error)
                *error = strdup("region is outside active displays");
            return NULL;
        }
        CGImageRef img = CGBitmapContextCreateImage(canvas);
        CGContextRelease(canvas);
        NSMutableData *data = [NSMutableData data];
        CGImageDestinationRef destination =
            CGImageDestinationCreateWithData((CFMutableDataRef)data, CFSTR("public.jpeg"), 1, NULL);
        CGImageDestinationAddImage(
            destination, img,
            (CFDictionaryRef)
                @{(id)kCGImageDestinationLossyCompressionQuality : @0.8});
        BOOL ok = CGImageDestinationFinalize(destination);
        CFRelease(destination);
        CGImageRelease(img);
        if (!ok) {
            *error = strdup("JPEG encoding failed");
            return NULL;
        }
        unsigned char *out = malloc(data.length);
        memcpy(out, data.bytes, data.length);
        *length = data.length;
        *out_width = (int)w;
        *out_height = (int)h;
        return out;
    }
}
