#import <Cocoa/Cocoa.h>

@interface Fixture : NSObject <NSApplicationDelegate>
@property(retain) NSWindow *window;
@property(retain) NSTextField *field;
@property(retain) NSWindow *cover;
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
    NSButton *cover = [NSButton buttonWithTitle:@"Fixture cover" target:self action:@selector(showCover:)];
    cover.frame = NSMakeRect(300, 100, 200, 40);
    [self.window.contentView addSubview:cover];
    [self.window makeKeyAndOrderFront:nil];
    [self.window makeFirstResponder:self.field];
    [NSApp activateIgnoringOtherApps:YES];
}
- (void)submit:(id)sender {
    self.field.stringValue = @"submitted";
    self.window.title = @"Computer MCP Native Fixture Updated";
}
- (void)showCover:(id)sender {
    self.cover = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 600, 300)
        styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable backing:NSBackingStoreBuffered defer:NO];
    self.cover.title = @"Computer MCP Fixture Cover";
    self.cover.backgroundColor = NSColor.redColor;
    [self.cover setFrame:self.window.frame display:YES];
    [self.cover makeKeyAndOrderFront:nil];
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
