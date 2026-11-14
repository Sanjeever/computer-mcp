#import <Cocoa/Cocoa.h>

@interface Fixture : NSObject <NSApplicationDelegate>
@property(retain) NSWindow *window;
@property(retain) NSTextField *field;
@end
@implementation Fixture
- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    self.window =
        [[NSWindow alloc] initWithContentRect:NSMakeRect(150, 150, 600, 300)
                                    styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
                                              NSWindowStyleMaskResizable
                                      backing:NSBackingStoreBuffered
                                        defer:NO];
    self.window.title = @"Computer MCP Native Fixture";
    self.field = [[NSTextField alloc] initWithFrame:NSMakeRect(40, 180, 500, 40)];
    self.field.accessibilityLabel = @"Fixture input";
    [self.window.contentView addSubview:self.field];
    NSButton *button = [NSButton buttonWithTitle:@"Fixture submit"
                                          target:self
                                          action:@selector(submit:)];
    button.frame = NSMakeRect(40, 100, 200, 40);
    [self.window.contentView addSubview:button];
    [self.window makeKeyAndOrderFront:nil];
    [self.window makeFirstResponder:self.field];
    [NSApp activateIgnoringOtherApps:YES];
}
- (void)submit:(id)sender {
    self.field.stringValue = @"submitted";
}
@end
int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        Fixture *delegate = [Fixture new];
        NSApp.delegate = delegate;
        [NSApp run];
    }
    return 0;
}
