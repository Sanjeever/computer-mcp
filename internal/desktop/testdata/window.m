#import <Cocoa/Cocoa.h>

@interface FixtureCanvas : NSView
@end
@implementation FixtureCanvas
- (BOOL)isAccessibilityElement { return YES; }
- (NSString *)accessibilityRole { return NSAccessibilityImageRole; }
- (void)drawRect:(NSRect)dirtyRect {
    [NSColor.whiteColor setFill];
    NSRectFill(self.bounds);
    [@"Canvas text 识别" drawAtPoint:NSMakePoint(10, 8)
        withAttributes:@{NSFontAttributeName: [NSFont systemFontOfSize:24], NSForegroundColorAttributeName: NSColor.blackColor}];
}
@end

@interface FixtureClickTarget : NSView
@property(assign) NSTextField *field;
@end
@implementation FixtureClickTarget
- (void)drawRect:(NSRect)dirtyRect {
    [NSColor.magentaColor setFill];
    NSRectFill(self.bounds);
}
- (void)mouseDown:(NSEvent *)event { self.field.stringValue = @"image clicked"; }
@end

@interface Fixture : NSObject <NSApplicationDelegate, NSTableViewDataSource>
@property(retain) NSWindow *window;
@property(retain) NSTextField *field;
@property(retain) NSWindow *cover;
@property(retain) NSWindow *editorWindow;
@property(retain) NSPanel *palette;
@end
@implementation Fixture
- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    NSMenu *menu = [NSMenu new];
    NSMenuItem *edit = [menu addItemWithTitle:@"Edit" action:nil keyEquivalent:@""];
    edit.submenu = [NSMenu new];
    [edit.submenu addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
    [edit.submenu addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];
    NSApp.mainMenu = menu;
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
    FixtureClickTarget *clickTarget = [[FixtureClickTarget alloc] initWithFrame:NSMakeRect(550, 20, 30, 30)];
    clickTarget.field = self.field;
    [self.window.contentView addSubview:clickTarget];
    NSButton *button = [NSButton buttonWithTitle:@"Fixture submit"
                                          target:self
                                          action:@selector(submit:)];
    button.frame = NSMakeRect(40, 100, 200, 40);
    [self.window.contentView addSubview:button];
    NSButton *cover = [NSButton buttonWithTitle:@"Fixture cover" target:self action:@selector(showCover:)];
    cover.frame = NSMakeRect(300, 100, 200, 40);
    [self.window.contentView addSubview:cover];
    [self.window.contentView addSubview:[[FixtureCanvas alloc] initWithFrame:NSMakeRect(40, 30, 500, 45)]];
    NSScrollView *scroll = [[NSScrollView alloc] initWithFrame:NSMakeRect(40, 0, 500, 25)];
    NSTableView *table = [[NSTableView alloc] initWithFrame:scroll.bounds];
    NSTableColumn *column = [[NSTableColumn alloc] initWithIdentifier:@"file"];
    column.width = 480;
    [table addTableColumn:column];
    table.headerView = nil;
    table.rowHeight = 20;
    table.dataSource = self;
    scroll.documentView = table;
    NSButton *cycle = [NSButton buttonWithTitle:@"Cycle" target:self action:@selector(cycle:)];
    cycle.frame = NSMakeRect(510, 100, 80, 40);
    [self.window.contentView addSubview:cycle];
    NSButton *modal = [NSButton buttonWithTitle:@"Modal" target:self action:@selector(modal:)];
    modal.frame = NSMakeRect(510, 145, 80, 35);
    [self.window.contentView addSubview:modal];
    NSButton *editor = [NSButton buttonWithTitle:@"Editor" target:self action:@selector(showEditor:)];
    editor.frame = NSMakeRect(510, 55, 80, 35);
    [self.window.contentView addSubview:editor];
    NSButton *palette = [NSButton buttonWithTitle:@"Palette" target:self action:@selector(showPalette:)];
    palette.frame = NSMakeRect(300, 150, 150, 30);
    [self.window.contentView addSubview:palette];
    [self.window.contentView addSubview:scroll];
    [self.window makeKeyAndOrderFront:nil];
    [self.window makeFirstResponder:self.field];
    [NSApp activateIgnoringOtherApps:YES];
}
- (void)submit:(id)sender {
    self.field.stringValue = @"submitted";
    self.window.title = @"Computer MCP Native Fixture Updated";
}
- (void)showPalette:(id)sender {
    self.palette = [[NSPanel alloc] initWithContentRect:NSMakeRect(800, 500, 180, 100)
        styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskUtilityWindow | NSWindowStyleMaskNonactivatingPanel
        backing:NSBackingStoreBuffered defer:NO];
    self.palette.title = @"Computer MCP Fixture Palette";
    self.palette.floatingPanel = YES;
    self.palette.becomesKeyOnlyIfNeeded = YES;
    NSButton *button = [NSButton buttonWithTitle:@"Palette action" target:self action:@selector(paletteClick:)];
    button.frame = NSMakeRect(20, 25, 140, 40);
    [self.palette.contentView addSubview:button];
    [self.palette orderFront:nil];
}
- (void)paletteClick:(id)sender { self.field.stringValue = @"palette clicked"; }
- (void)showEditor:(id)sender {
    self.editorWindow = [[NSWindow alloc] initWithContentRect:NSMakeRect(250, 200, 600, 500)
        styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskResizable
        backing:NSBackingStoreBuffered defer:NO];
    self.editorWindow.title = @"Computer MCP Fixture Editor";
    NSScrollView *scroll = [[NSScrollView alloc] initWithFrame:self.editorWindow.contentView.bounds];
    scroll.hasVerticalScroller = YES;
    scroll.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    NSTextView *editor = [[NSTextView alloc] initWithFrame:scroll.bounds];
    editor.accessibilityLabel = @"Fixture editor";
    editor.verticallyResizable = YES;
    editor.autoresizingMask = NSViewWidthSizable;
    editor.textContainer.widthTracksTextView = YES;
    editor.automaticQuoteSubstitutionEnabled = NO;
    editor.automaticDashSubstitutionEnabled = NO;
    editor.automaticTextReplacementEnabled = NO;
    scroll.documentView = editor;
    self.editorWindow.contentView = scroll;
    [self.editorWindow makeKeyAndOrderFront:nil];
    [self.editorWindow makeFirstResponder:editor];
}
- (void)showCover:(id)sender {
    self.cover = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 600, 300)
        styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable backing:NSBackingStoreBuffered defer:NO];
    self.cover.title = @"Computer MCP Fixture Cover";
    self.cover.backgroundColor = NSColor.redColor;
    [self.cover setFrame:self.window.frame display:YES];
    [self.cover makeKeyAndOrderFront:nil];
}
- (NSInteger)numberOfRowsInTableView:(NSTableView *)tableView { return 300; }
- (id)tableView:(NSTableView *)tableView objectValueForTableColumn:(NSTableColumn *)column row:(NSInteger)row {
    return [NSString stringWithFormat:@"File %03ld.txt", (long)row];
}
- (void)cycle:(id)sender {
    [self.window orderOut:nil];
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 500 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
        [self.window makeKeyAndOrderFront:nil];
        [NSApp activateIgnoringOtherApps:YES];
    });
}
- (void)modal:(id)sender {
    NSAlert *alert = [NSAlert new];
    alert.messageText = @"Fixture modal";
    [alert beginSheetModalForWindow:self.window completionHandler:nil];
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC), dispatch_get_main_queue(), ^{
        [self.window endSheet:alert.window];
        [alert.window orderOut:nil];
        [alert release];
    });
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
