//go:build darwin

// sc_app.m is the Cocoa + MetalKit host and the native Metal renderer.
// Manual reference counting is used because this file is compiled without ARC.
#import "sc_app.h"
#include "_cgo_export.h"
#import <Cocoa/Cocoa.h>
#import <Metal/Metal.h>
#import <MetalKit/MetalKit.h>
#import <QuartzCore/CAMetalLayer.h>
#include <dispatch/dispatch.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static NSString *gTitle = @"ScottiCAD";
static int gW = 1280, gH = 800, gVisible = 1;
static double gScale = 0, gClear[4] = {0.12, 0.13, 0.15, 1};
static id<MTLDevice> gDevice;
static id<MTLCommandQueue> gQueue;
static NSWindow *gWindow;
static MTKView *gView;
static id gDelegate;
static NSTimer *gIdleTimer;
static id<CAMetalDrawable> gDrawable;
static id<MTLCommandBuffer> gCmdBuf;
static id<MTLRenderCommandEncoder> gEnc;
static id<MTLSamplerState> gLinearSampler, gNearestSampler;
static dispatch_semaphore_t gFrameSemaphore;
static int gFrameSlot = 2;
static BOOL gFramePermit, gFrameActive;
static NSMutableSet *gBuffers;
static int gDrawW, gDrawH, gTargetW, gTargetH;
static uintptr_t gCurrentTarget;
static int gDepthTest, gDepthWrite = 1, gDepthLEqual = 1, gBlend, gCull;
static int gAcceptDrop;
static BOOL gCloseApproved, gTerminating;
static NSEventModifierFlags gModifierFlags;
static BOOL gHaveModifierFlags;
static uintptr_t gBoundBuffer, gBoundTexture;
static BOOL gScissorEnabled;
static MTLScissorRect gScissorRect;
static int gScissorArgs[4];
static NSMutableSet *gTargets;
static NSMutableSet *gNearestTextures;
static NSMutableDictionary *gClearPipelines;
static id<MTLLibrary> gClearLibrary;
static BOOL SCEnsureEncoder(void);
static void SCEndEncoder(void);
static void SCUpdateDrawable(void);
static int SCChooseSampleCount(int requested);
static void SCSetScissorForEncoder(void);
static id<MTLRenderPipelineState> SCClearPipeline(int samples, BOOL hasDepth, BOOL color);
typedef struct SCTarget {
	id<MTLTexture> color, resolve, depth;
	int width, height, samples, hasDepth, nearest;
} SCTarget;

typedef struct SCBuffer {
	id<MTLBuffer> base;
	NSUInteger baseSize;
	id<MTLBuffer> *pages[3];
	NSUInteger pageCount[3], pageCapacity[3];
	BOOL initialized, dynamic;
} SCBuffer;

typedef struct {
	int stage, index, offset, size;
} SCUniform;

@interface SCProgram : NSObject {
@public
	id<MTLLibrary> library;
	id<MTLFunction> vertexFunction, fragmentFunction;
	NSMutableDictionary *uniformSlots, *uniformData;
	NSMutableArray *uniforms;
	NSMutableDictionary *pipelineCache;
	NSString *kind;
	MTLVertexDescriptor *vertexDescriptor;
}
- (id<MTLRenderPipelineState>)pipelineForState;
- (void)bindUniformData;
- (void)dealloc;
@end

@implementation SCProgram
- (id<MTLRenderPipelineState>)pipelineForState {
	int samples = gCurrentTarget ? ((SCTarget *)(void *)gCurrentTarget)->samples : 1;
	BOOL hasDepth = gCurrentTarget == 0
		? (gView != nil && gView.depthStencilPixelFormat != MTLPixelFormatInvalid)
		: (((SCTarget *)(void *)gCurrentTarget)->depth != nil);
	int key = (gBlend ? 1 : 0) | (hasDepth ? 2 : 0) | (samples << 2);
	NSNumber *cacheKey = [NSNumber numberWithInt:key];
	id<MTLRenderPipelineState> cached = [pipelineCache objectForKey:cacheKey];
	if (cached != nil) return cached;
	MTLRenderPipelineDescriptor *d = [[MTLRenderPipelineDescriptor alloc] init];
	d.vertexFunction = vertexFunction;
	d.fragmentFunction = fragmentFunction;
	d.vertexDescriptor = vertexDescriptor;
	d.colorAttachments[0].pixelFormat = MTLPixelFormatBGRA8Unorm;
	d.depthAttachmentPixelFormat = hasDepth ? MTLPixelFormatDepth32Float : MTLPixelFormatInvalid;
	d.rasterSampleCount = (NSUInteger)samples;
	if (gBlend) {
		MTLRenderPipelineColorAttachmentDescriptor *ca = d.colorAttachments[0];
		ca.blendingEnabled = YES;
		ca.sourceRGBBlendFactor = MTLBlendFactorSourceAlpha;
		ca.destinationRGBBlendFactor = MTLBlendFactorOneMinusSourceAlpha;
		ca.sourceAlphaBlendFactor = MTLBlendFactorSourceAlpha;
		ca.destinationAlphaBlendFactor = MTLBlendFactorOneMinusSourceAlpha;
	}
	NSError *err = nil;
	id<MTLRenderPipelineState> ps = [gDevice newRenderPipelineStateWithDescriptor:d error:&err];
	if (ps == nil) {
		fprintf(stderr, "scotticad: %s pipeline failed: %s\n",
		        [kind UTF8String], err ? [[err localizedDescription] UTF8String] : "unknown");
		[d release];
		return nil;
	}
	[pipelineCache setObject:ps forKey:cacheKey];
	[ps release];
	[d release];
	return [pipelineCache objectForKey:cacheKey];
}
- (void)bindUniformData {
	for (NSString *key in uniformData) {
		NSData *data = [uniformData objectForKey:key];
		NSArray *parts = [key componentsSeparatedByString:@":"];
		if ([parts count] != 2 || [data length] == 0) continue;
		int stage = [[parts objectAtIndex:0] intValue];
		int index = [[parts objectAtIndex:1] intValue];
		if (stage == 0)
			[gEnc setVertexBytes:[data bytes] length:[data length] atIndex:(NSUInteger)index];
		else
			[gEnc setFragmentBytes:[data bytes] length:[data length] atIndex:(NSUInteger)index];
	}
}
- (void)dealloc {
	[library release];
	[vertexFunction release];
	[fragmentFunction release];
	[uniformSlots release];
	[uniformData release];
	[uniforms release];
	[pipelineCache release];
	[kind release];
	[vertexDescriptor release];
	[super dealloc];
}
@end

static SCProgram *gProgram;
static uintptr_t gProgramHandles[64];
static int gProgramCount;
static int gLastViewport[4];
static BOOL gHasViewport;

static NSString *SCString(const char *s, NSString *fallback) {
	if (s == NULL) return fallback;
	NSString *v = [NSString stringWithUTF8String:s];
	return v ?: fallback;
}

static void SCLogError(const char *what, NSError *err) {
	fprintf(stderr, "scotticad: %s: %s\n", what,
	        err ? [[err localizedDescription] UTF8String] : "unknown error");
}

static double SCBackingScale(void) {
	double s = 0;
	if (gWindow != nil) s = gWindow.backingScaleFactor;
	if (s <= 0 && gView != nil) s = gView.window.backingScaleFactor;
	return s > 0 ? s : 1;
}

static double SCInterfaceScale(void) {
	return gScale > 0 ? gScale : SCBackingScale();
}

static int SCVK(unsigned short k) {
	switch (k) {
	case 0: return 'A'; case 1: return 'S'; case 2: return 'D'; case 3: return 'F';
	case 4: return 'H'; case 5: return 'G'; case 6: return 'Z'; case 7: return 'X';
	case 8: return 'C'; case 9: return 'V'; case 11: return 'B'; case 12: return 'Q';
	case 13: return 'W'; case 14: return 'E'; case 15: return 'R'; case 16: return 'Y';
	case 17: return 'T'; case 18: return '1'; case 19: return '2'; case 20: return '3';
	case 21: return '4'; case 22: return '6'; case 23: return '5'; case 24: return 0xBB;
	case 25: return '9'; case 26: return '7'; case 27: return 0xBD; case 28: return '8';
	case 29: return '0'; case 30: return ']'; case 31: return 'O'; case 32: return 'U';
	case 33: return '['; case 34: return 'I'; case 35: return 'P'; case 36: return 0x0D;
	case 37: return 'L'; case 38: return 'J'; case 39: return '\''; case 40: return 'K';
	case 41: return ';'; case 42: return '\\'; case 43: return 0xBC; case 44: return 0xBF;
	case 45: return 'N'; case 46: return 'M'; case 47: return 0xBE; case 48: return 0x09;
	case 49: return 0x20; case 50: return 0xC0; case 51: return 0x08; case 53: return 0x1B;
	case 54: return 0x11; case 55: return 0x11; case 56: return 0x10; case 57: return 0x14;
	case 58: return 0x12; case 59: return 0x11; case 60: return 0x10; case 61: return 0x12;
	case 62: return 0x11; case 63: return 0;
	case 65: return 0xBE; case 67: return 0x6A; case 69: return 0x6B; case 71: return 0x6C;
	case 75: return 0x6F; case 76: return 0x0D; case 78: return 0x6D; case 81: return 0xBB;
	case 82: return '0'; case 83: return '1'; case 84: return '2'; case 85: return '3';
	case 86: return '4'; case 87: return '5'; case 88: return '6'; case 89: return '7';
	case 91: return '8'; case 92: return '9'; case 96: return 0x74; case 97: return 0x75;
	case 98: return 0x76; case 99: return 0x72; case 100: return 0x77; case 101: return 0x78;
	case 103: return 0x7A; case 109: return 0x79; case 111: return 0x7B; case 120: return 0x71;
	case 118: return 0x73; case 122: return 0x70; case 115: return 0x24; case 116: return 0x21; case 117: return 0x2E;
	case 119: return 0x23; case 121: return 0x22; case 123: return 0x25; case 124: return 0x27;
	case 125: return 0x28; case 126: return 0x26; default: return 0;
	}
}

static int SCMods(NSEventModifierFlags flags) {
	int m = 0;
	if (flags & NSEventModifierFlagShift) m |= 1;
	if (flags & (NSEventModifierFlagCommand | NSEventModifierFlagControl)) m |= 2;
	if (flags & NSEventModifierFlagOption) m |= 4;
	return m;
}

static void SCPoint(NSView *view, NSEvent *event, double *x, double *y) {
	NSPoint p = [view convertPoint:event.locationInWindow fromView:nil];
	double scale = SCBackingScale();
	*x = p.x * scale;
	*y = (NSHeight(view.bounds) - p.y) * scale;
}

@interface SCMetalView : MTKView <NSDraggingDestination>
@end
@implementation SCMetalView
- (BOOL)acceptsFirstResponder { return YES; }
- (void)setFrameSize:(NSSize)size {
	[super setFrameSize:size];
	if (gView == self) SCUpdateDrawable();
}
- (void)viewDidChangeBackingProperties {
	[super viewDidChangeBackingProperties];
	SCUpdateDrawable();
}
- (void)mouseMoved:(NSEvent *)e {
	double x, y; SCPoint(self, e, &x, &y); goEventMouseMove(x, y);
}
- (void)mouseDragged:(NSEvent *)e { [self mouseMoved:e]; }
- (void)rightMouseDragged:(NSEvent *)e { [self mouseMoved:e]; }
- (void)otherMouseDragged:(NSEvent *)e { [self mouseMoved:e]; }
- (void)mouseDown:(NSEvent *)e {
	double x, y; SCPoint(self, e, &x, &y);
	goEventMouseDown(0, x, y, SCMods(e.modifierFlags), (int)e.clickCount);
}
- (void)rightMouseDown:(NSEvent *)e {
	double x, y; SCPoint(self, e, &x, &y);
	goEventMouseDown(1, x, y, SCMods(e.modifierFlags), (int)e.clickCount);
}
- (void)otherMouseDown:(NSEvent *)e {
	double x, y; SCPoint(self, e, &x, &y);
	goEventMouseDown(2, x, y, SCMods(e.modifierFlags), (int)e.clickCount);
}
- (void)mouseUp:(NSEvent *)e {
	double x, y; SCPoint(self, e, &x, &y); goEventMouseUp(0, x, y, SCMods(e.modifierFlags));
}
- (void)rightMouseUp:(NSEvent *)e {
	double x, y; SCPoint(self, e, &x, &y); goEventMouseUp(1, x, y, SCMods(e.modifierFlags));
}
- (void)otherMouseUp:(NSEvent *)e {
	double x, y; SCPoint(self, e, &x, &y); goEventMouseUp(2, x, y, SCMods(e.modifierFlags));
}
- (void)scrollWheel:(NSEvent *)e {
	double x, y; SCPoint(self, e, &x, &y);
	goEventWheel(x, y, e.scrollingDeltaX, e.scrollingDeltaY);
}
- (void)mouseExited:(NSEvent *)e { goEventMouseLeave(); }
- (void)keyDown:(NSEvent *)e {
	int vk = SCVK(e.keyCode);
	if (vk != 0) goEventKeyDown(vk, SCMods(e.modifierFlags), e.isARepeat);
	if (!(e.modifierFlags & (NSEventModifierFlagCommand | NSEventModifierFlagControl | NSEventModifierFlagOption)))
		[self interpretKeyEvents:@[e]];
}
- (void)keyUp:(NSEvent *)e {
	int vk = SCVK(e.keyCode);
	if (vk != 0) goEventKeyUp(vk, SCMods(e.modifierFlags));
}
- (void)flagsChanged:(NSEvent *)e {
	int vk = SCVK(e.keyCode);
	if (vk == 0) return;
	NSEventModifierFlags mask = 0;
	switch (e.keyCode) {
	case 54: case 55: case 59: case 62:
		mask = NSEventModifierFlagCommand | NSEventModifierFlagControl;
		break;
	case 56: case 60: mask = NSEventModifierFlagShift; break;
	case 58: case 61: mask = NSEventModifierFlagOption; break;
	case 57: mask = NSEventModifierFlagCapsLock; break;
	default: return;
	}
	BOOL wasDown = gHaveModifierFlags && (gModifierFlags & mask) != 0;
	BOOL down = (e.modifierFlags & mask) != 0;
	gModifierFlags = e.modifierFlags;
	gHaveModifierFlags = YES;
	if (wasDown == down) return;
	if (down)
		goEventKeyDown(vk, SCMods(e.modifierFlags), 0);
	else
		goEventKeyUp(vk, SCMods(e.modifierFlags));
}
- (void)insertText:(id)value replacementRange:(NSRange)range {
	NSString *s = [value isKindOfClass:[NSAttributedString class]] ? [value string] : value;
	if (![s isKindOfClass:[NSString class]]) return;
	for (NSUInteger i = 0; i < [s length]; i++) {
		unichar c = [s characterAtIndex:i];
		if (c >= 0xD800 && c <= 0xDBFF && i + 1 < [s length]) {
			unichar lo = [s characterAtIndex:i + 1];
			if (lo >= 0xDC00 && lo <= 0xDFFF) {
				int scalar = 0x10000 + (((int)c - 0xD800) << 10) + ((int)lo - 0xDC00);
				goEventChar(scalar); i++;
				continue;
			}
		}
		goEventChar((int)c);
	}
}
- (NSDragOperation)draggingEntered:(id<NSDraggingInfo>)sender {
	return gAcceptDrop ? NSDragOperationCopy : NSDragOperationNone;
}
- (BOOL)prepareForDragOperation:(id<NSDraggingInfo>)sender { return gAcceptDrop; }
- (BOOL)performDragOperation:(id<NSDraggingInfo>)sender {
	if (!gAcceptDrop) return NO;
	NSArray *urls = [[sender draggingPasteboard] readObjectsForClasses:@[[NSURL class]]
	                                                           options:@{NSPasteboardURLReadingFileURLsOnlyKey:@YES}];
	if ([urls count] == 0) return NO;
	NSMutableArray *paths = [NSMutableArray array];
	for (NSURL *url in urls) if (url.isFileURL) [paths addObject:url.path];
	if ([paths count] == 0) return NO;
	NSString *joined = [paths componentsJoinedByString:@"\n"];
	NSData *utf8 = [joined dataUsingEncoding:NSUTF8StringEncoding];
	char *copy = malloc([utf8 length] + 1);
	if (copy == NULL) return NO;
	memcpy(copy, [utf8 bytes], [utf8 length]); copy[[utf8 length]] = 0;
	NSPoint p = [self convertPoint:sender.draggingLocation fromView:nil];
	double scale = self.window.backingScaleFactor > 0 ? self.window.backingScaleFactor : 1;
	double x = p.x * scale, y = (NSHeight(self.bounds) - p.y) * scale;
	goEventDrop(copy, x, y);
	free(copy);
	return YES;
}
@end

@interface SCAppDelegate : NSObject <NSApplicationDelegate, NSWindowDelegate, MTKViewDelegate>
- (void)installMenu;
- (void)setupWindow;
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender;
@end
@implementation SCAppDelegate
- (void)applicationDidFinishLaunching:(NSNotification *)note { if (!gWindow) [self setupWindow]; }
- (void)setupWindow {
	gDevice = MTLCreateSystemDefaultDevice();
if (gDevice == nil) { fprintf(stderr, "scotticad: no Metal device available\n"); [NSApp terminate:nil]; return; }
	gQueue = [gDevice newCommandQueue];
	if (gQueue == nil) { fprintf(stderr, "scotticad: cannot create Metal command queue\n"); [NSApp terminate:nil]; return; }
	gFrameSemaphore = dispatch_semaphore_create(3);
	gBuffers = [[NSMutableSet alloc] init];
	// A visible window is clamped to the screen so it can be used; a hidden
	// one (automated tests) keeps exactly the size asked for, whatever the
	// display is, or the laid-out controls don't match the test's window.
	NSScreen *screen = gVisible ? [NSScreen mainScreen] : nil;
	if (screen != nil) {
		NSRect visible = screen.visibleFrame;
		gW = MIN(gW, MAX(1, (int)visible.size.width - 80));
		gH = MIN(gH, MAX(1, (int)visible.size.height - 80));
	}
	NSRect frame = NSMakeRect(0, 0, gW, gH);
	NSWindowStyleMask style = NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
		NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable;
	gWindow = [[NSWindow alloc] initWithContentRect:frame styleMask:style
	                                      backing:NSBackingStoreBuffered defer:NO];
	gWindow.title = gTitle; gWindow.delegate = self; [gWindow center];
	SCMetalView *view = [[SCMetalView alloc] initWithFrame:frame device:gDevice];
	gView = view;
	gView.colorPixelFormat = MTLPixelFormatBGRA8Unorm;
	gView.depthStencilPixelFormat = MTLPixelFormatDepth32Float;
	gView.preferredFramesPerSecond = 60;
	gView.paused = YES;
	gView.enableSetNeedsDisplay = YES;
	gView.autoResizeDrawable = NO;
	gView.clearColor = MTLClearColorMake(gClear[0], gClear[1], gClear[2], gClear[3]);
	gView.delegate = self;
	gWindow.contentView = gView;
	gWindow.acceptsMouseMovedEvents = YES;
	[gView registerForDraggedTypes:@[NSPasteboardTypeFileURL]];
	NSTrackingArea *tracking = [[NSTrackingArea alloc] initWithRect:NSZeroRect
		options:NSTrackingMouseMoved | NSTrackingMouseEnteredAndExited |
		        NSTrackingActiveInKeyWindow | NSTrackingInVisibleRect
		owner:gView userInfo:nil];
	[gView addTrackingArea:tracking];
	[self installMenu];
	gIdleTimer = [NSTimer scheduledTimerWithTimeInterval:(1.0/60.0) repeats:YES
	                                              block:^(NSTimer *timer) { goEventIdle(); }];
	if (gVisible) { [gWindow makeKeyAndOrderFront:nil]; [NSApp activateIgnoringOtherApps:YES]; }
	SCUpdateDrawable();
}
- (void)installMenu {
	NSMenu *bar = [[NSMenu alloc] init];
	NSMenuItem *item = [[NSMenuItem alloc] init]; [bar addItem:item]; [NSApp setMainMenu:bar];
	NSString *name = [[NSProcessInfo processInfo] processName];
	NSMenu *menu = [[NSMenu alloc] init];
	[menu addItemWithTitle:[@"About " stringByAppendingString:name] action:@selector(orderFrontStandardAboutPanel:) keyEquivalent:@""];
	[menu addItem:[NSMenuItem separatorItem]];
	[menu addItemWithTitle:[@"Hide " stringByAppendingString:name] action:@selector(hide:) keyEquivalent:@"h"];
	[menu addItem:[NSMenuItem separatorItem]];
	[menu addItemWithTitle:[@"Quit " stringByAppendingString:name] action:@selector(terminate:) keyEquivalent:@"q"];
	item.submenu = menu;
}
- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)app { return YES; }
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
	if (gTerminating || gCloseApproved) {
		gTerminating = NO;
		gCloseApproved = NO;
		return NSTerminateNow;
	}
	return goEventClose() ? NSTerminateNow : NSTerminateCancel;
}
- (BOOL)windowShouldClose:(NSWindow *)sender {
	if (goEventClose() == 0) return NO;
	gCloseApproved = YES;
	return YES;
}
- (void)windowWillClose:(NSNotification *)note { [NSApp terminate:nil]; }
- (void)windowDidResignKey:(NSNotification *)note {
	gHaveModifierFlags=NO;
	goEventCapture(1);
}
- (void)mtkView:(MTKView *)view drawableSizeWillChange:(CGSize)size {
	goResize((int)size.width, (int)size.height, SCBackingScale());
}
- (void)drawInMTKView:(MTKView *)view { goDraw(); }
@end

static void SCUpdateDrawable(void) {
	if (!gView || gView.autoResizeDrawable) return;
	double scale = SCBackingScale();
	CGSize size = gView.bounds.size;
	gView.drawableSize = CGSizeMake(size.width * scale, size.height * scale);
	goResize((int)gView.drawableSize.width, (int)gView.drawableSize.height, SCInterfaceScale());
}

static int SCParseList(const char *s, char ***out) {
	*out = NULL;
	if (s == NULL || *s == 0) return 0;
	char *copy = strdup(s), *save = NULL;
	if (copy == NULL) return 0;
	int n = 0;
	for (char *line = strtok_r(copy, "\n", &save); line != NULL; line = strtok_r(NULL, "\n", &save)) {
		char **p = realloc(*out, sizeof(char *) * (n + 1));
		if (p == NULL) break;
		*out = p; (*out)[n++] = strdup(line);
	}
	free(copy); return n;
}

static NSURL *SCChooseFile(BOOL save, const char *title, const char *filters, const char *defname) {
	NSSavePanel *panel = save ? [NSSavePanel savePanel] : [NSOpenPanel openPanel];
	panel.title = SCString(title, save ? @"Save" : @"Open");
	panel.canCreateDirectories = YES;
	if (!save) { ((NSOpenPanel *)panel).canChooseFiles = YES; ((NSOpenPanel *)panel).canChooseDirectories = NO; ((NSOpenPanel *)panel).allowsMultipleSelection = NO; }
	if (defname != NULL) panel.nameFieldStringValue = SCString(defname, @"");
	char **lines = NULL; int n = SCParseList(filters, &lines);
	NSMutableArray *types = [NSMutableArray array];
	BOOL unrestricted = NO;
	for (int i = 0; i < n; i++) {
		char *bar = strchr(lines[i], '|');
		const char *patterns = bar == NULL ? lines[i] : bar + 1;
		char *patcopy = strdup(patterns), *saveptr = NULL;
		for (char *p = strtok_r(patcopy, ";", &saveptr); p != NULL; p = strtok_r(NULL, ";", &saveptr)) {
			while (*p == ' ') p++;
			if (strcmp(p, "*") == 0 || strcmp(p, "*.*") == 0) {
				unrestricted = YES;
			} else if (p[0] == '*' && p[1] == '.' && p[2] != 0) {
				[types addObject:[NSString stringWithUTF8String:p + 2]];
			}
		}
		free(patcopy); free(lines[i]);
	}
	free(lines);
	if (!unrestricted && [types count] > 0) panel.allowedFileTypes = types;
	if ([panel runModal] != NSModalResponseOK) return nil;
	return panel.URL;
}

void sc_set_scale(double scale) { gScale = scale; }
double sc_backing_scale(void) { return SCBackingScale(); }
void sc_start(const char *title, int w, int h, int visible) {
	@autoreleasepool {
		gTitle = [SCString(title, @"ScottiCAD") copy];
		gW = w > 0 ? w : 1280; gH = h > 0 ? h : 800; gVisible = visible;
		[NSApplication sharedApplication]; [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
		gDelegate = [[SCAppDelegate alloc] init]; NSApp.delegate = gDelegate;
		[gDelegate setupWindow];
	}
}
void sc_runloop(void) { [NSApp run]; }
void sc_end(void) { dispatch_async(dispatch_get_main_queue(), ^{ gTerminating = YES; [NSApp terminate:nil]; }); }
void sc_set_title(const char *title) {
	if (title == NULL) return; char *copy = strdup(title); if (!copy) return;
	dispatch_async(dispatch_get_main_queue(), ^{ if (gWindow) gWindow.title = SCString(copy, @"ScottiCAD"); free(copy); });
}
void sc_show(void) { dispatch_async(dispatch_get_main_queue(), ^{ if (gWindow) { [gWindow makeKeyAndOrderFront:nil]; [NSApp activateIgnoringOtherApps:YES]; } }); }
void sc_set_min_size(int w, int h) { if (gWindow) gWindow.contentMinSize = NSMakeSize(w, h); }
void sc_request_frame(void) { if (gView) dispatch_async(dispatch_get_main_queue(), ^{ [gView setNeedsDisplay:YES]; }); }

void sc_set_clipboard(const char *s) {
	NSPasteboard *p = [NSPasteboard generalPasteboard]; [p clearContents];
	if (s) [p setString:SCString(s, @"") forType:NSPasteboardTypeString];
}
char *sc_get_clipboard(void) {
	NSString *s = [[NSPasteboard generalPasteboard] stringForType:NSPasteboardTypeString];
	if (!s) return NULL; const char *u = [s UTF8String]; return u ? strdup(u) : NULL;
}
void sc_set_cursor(int id) {
	NSCursor *c = [NSCursor arrowCursor];
	switch (id) {
	case 32513: c = [NSCursor IBeamCursor]; break;
	case 32514: c = [NSCursor disappearingItemCursor]; break;
	case 32515: c = [NSCursor crosshairCursor]; break;
	case 32644: c = [NSCursor resizeLeftRightCursor]; break;
	case 32645: c = [NSCursor resizeUpDownCursor]; break;
	case 32649: c = [NSCursor pointingHandCursor]; break;
	case 32648: c = [NSCursor operationNotAllowedCursor]; break;
	default: break;
	}
	[c set];
}
void sc_set_icon(const void *rgba, int size, int big) {
	if (!rgba || size <= 0) return;
	NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithBitmapDataPlanes:NULL pixelsWide:size pixelsHigh:size
		bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO colorSpaceName:NSDeviceRGBColorSpace
		bytesPerRow:size * 4 bitsPerPixel:32];
	memcpy(rep.bitmapData, rgba, (size_t)size * (size_t)size * 4);
	NSImage *img = [[NSImage alloc] initWithSize:NSMakeSize(size, size)];
	[img addRepresentation:rep]; [NSApp setApplicationIconImage:img];
	[img release]; [rep release];
	(void)big;
}
char *sc_open_file(const char *title, const char *filters) {
	NSURL *url = SCChooseFile(NO, title, filters, NULL); return url ? strdup(url.fileSystemRepresentation) : NULL;
}
char *sc_save_file(const char *title, const char *filters, const char *defname) {
	NSURL *url = SCChooseFile(YES, title, filters, defname); return url ? strdup(url.fileSystemRepresentation) : NULL;
}
int sc_message(const char *title, const char *text, int kind, int buttons) {
	NSAlert *a = [[NSAlert alloc] init]; a.messageText = SCString(title, @"ScottiCAD"); a.informativeText = SCString(text, @"");
	a.alertStyle = kind == 2 ? NSAlertStyleCritical : (kind == 1 ? NSAlertStyleWarning : NSAlertStyleInformational);
	if (buttons == 3) { [a addButtonWithTitle:@"Yes"]; [a addButtonWithTitle:@"No"]; [a addButtonWithTitle:@"Cancel"]; }
	else if (buttons == 4) { [a addButtonWithTitle:@"Yes"]; [a addButtonWithTitle:@"No"]; }
	else [a addButtonWithTitle:@"OK"];
	NSModalResponse r = [a runModal];
	int result;
	if (buttons == 3) result = r == NSAlertFirstButtonReturn ? 6 : (r == NSAlertSecondButtonReturn ? 7 : 2);
	else if (buttons == 4) result = r == NSAlertFirstButtonReturn ? 6 : 7;
	else result = 1;
	[a release];
	return result;
}
void sc_open_url(const char *url) { if (url) [[NSWorkspace sharedWorkspace] openURL:[NSURL URLWithString:SCString(url, @"")]]; }
void sc_accept_drop(int on) { gAcceptDrop = on != 0; }

int sc_device_ready(void) { return gDevice && gQueue ? 1 : 0; }

static int SCParseInt(char **p) { return (int)strtol(*p, p, 10); }
uintptr_t sc_program(const char *kind, const char *msl, const char *vs, const char *fs,
                     const char *uniformSpec, const char *attrSpec) {
	@autoreleasepool {
	if (!gDevice || !msl || !vs || !fs) return 0;
	NSError *err = nil;
	SCProgram *p = [[SCProgram alloc] init];
	p->kind = [SCString(kind, @"program") copy];
	p->uniformSlots = [[NSMutableDictionary alloc] init];
	p->uniformData = [[NSMutableDictionary alloc] init];
	p->uniforms = [[NSMutableArray alloc] init];
	p->pipelineCache = [[NSMutableDictionary alloc] init];
	p->vertexDescriptor = [[MTLVertexDescriptor alloc] init];
	p->library = [gDevice newLibraryWithSource:SCString(msl, @"") options:nil error:&err];
	if (!p->library) { SCLogError("Metal shader compilation failed", err); [p release]; return 0; }
	p->vertexFunction = [p->library newFunctionWithName:SCString(vs, @"")];
	p->fragmentFunction = [p->library newFunctionWithName:SCString(fs, @"")];
	if (!p->vertexFunction || !p->fragmentFunction) {
		fprintf(stderr, "scotticad: Metal functions %s/%s not found\n", vs, fs); [p release]; return 0;
	}
	int maxSlots = 0;
	if (uniformSpec && *uniformSpec) {
		char *copy = strdup(uniformSpec), *save = NULL;
		for (char *entry = strtok_r(copy, ";", &save); entry; entry = strtok_r(NULL, ";", &save)) {
			char *s = entry, *name = strsep(&s, ":");
			if (!name || !s) continue;
			SCUniform u; u.stage = SCParseInt(&s); if (*s == ':') s++;
			u.index = SCParseInt(&s); if (*s == ':') s++;
			u.offset = SCParseInt(&s); if (*s == ':') s++;
			u.size = SCParseInt(&s);
			[p->uniforms addObject:[NSValue valueWithBytes:&u objCType:@encode(SCUniform)]];
			[p->uniformSlots setObject:[NSNumber numberWithInt:maxSlots] forKey:SCString(name, @"")];
			NSString *key = [NSString stringWithFormat:@"%d:%d", u.stage, u.index];
			NSMutableData *data = [p->uniformData objectForKey:key];
			NSUInteger need = (NSUInteger)(u.offset + u.size);
			if (!data || [data length] < need) {
				NSMutableData *larger = [NSMutableData dataWithLength:need];
				if (data) memcpy([larger mutableBytes], [data bytes], [data length]);
				[p->uniformData setObject:larger forKey:key];
			}
			maxSlots++;
		}
		free(copy);
	}
	if (attrSpec && *attrSpec) {
		char *copy = strdup(attrSpec), *save = NULL;
		for (char *entry = strtok_r(copy, ";", &save); entry; entry = strtok_r(NULL, ";", &save)) {
			char *s = entry;
			int stride = SCParseInt(&s); if (*s == ':') s++;
			int loc = SCParseInt(&s); if (*s == ':') s++;
			int n = SCParseInt(&s); if (*s == ':') s++;
			int offset = SCParseInt(&s);
			MTLVertexFormat fmt = n == 1 ? MTLVertexFormatFloat : n == 2 ? MTLVertexFormatFloat2 :
				n == 3 ? MTLVertexFormatFloat3 : MTLVertexFormatFloat4;
			p->vertexDescriptor.attributes[loc].format = fmt;
			p->vertexDescriptor.attributes[loc].offset = (NSUInteger)offset;
			p->vertexDescriptor.attributes[loc].bufferIndex = 0;
			p->vertexDescriptor.layouts[0].stride = (NSUInteger)stride;
			p->vertexDescriptor.layouts[0].stepFunction = MTLVertexStepFunctionPerVertex;
		}
		free(copy);
	}
	if ([p pipelineForState] == nil) { [p release]; return 0; }
	uintptr_t handle = (uintptr_t)p;
	if (gProgramCount < 64) gProgramHandles[gProgramCount++] = handle;
	return handle;
	}
}
void sc_program_free(uintptr_t h) { if (h) [(SCProgram *)(void *)h release]; if ((uintptr_t)gProgram == h) gProgram = nil; }
int sc_uniform(uintptr_t h, const char *name) {
	SCProgram *p = (SCProgram *)(void *)h; if (!p || !name) return -1;
	NSNumber *n = [p->uniformSlots objectForKey:SCString(name, @"")]; return n ? [n intValue] : -1;
}
void sc_use_program(uintptr_t h) {
	if (!gEnc || !h) return;
	gProgram = (SCProgram *)(void *)h;
	id<MTLRenderPipelineState> ps = [gProgram pipelineForState];
	if (ps) [gEnc setRenderPipelineState:ps];
	[gProgram bindUniformData];
}
static SCUniform *SCGetUniform(int loc, SCUniform *out) {
	if (!gProgram || loc < 0 || loc >= (int)[gProgram->uniforms count]) return NULL;
	[[gProgram->uniforms objectAtIndex:(NSUInteger)loc] getValue:out];
	return out;
}
static void SCSetUniform(int loc, const void *value, size_t n) {
	SCUniform u; if (!SCGetUniform(loc, &u) || u.size <= 0 || !SCEnsureEncoder()) return;
	NSString *key = [NSString stringWithFormat:@"%d:%d", u.stage, u.index];
	NSMutableData *data = [gProgram->uniformData objectForKey:key];
	if (!data || u.offset < 0 || (NSUInteger)u.offset + n > [data length]) return;
	memcpy((char *)[data mutableBytes] + u.offset, value, MIN(n, (size_t)u.size));
	if (u.stage == 0) [gEnc setVertexBytes:[data bytes] length:[data length] atIndex:(NSUInteger)u.index];
	else [gEnc setFragmentBytes:[data bytes] length:[data length] atIndex:(NSUInteger)u.index];
}
void sc_uniform1i(int loc, int v) { SCUniform u; if (!SCGetUniform(loc, &u)) return; if (u.size == 8) return; SCSetUniform(loc, &v, sizeof(v)); }
void sc_uniform1f(int loc, float v) { SCSetUniform(loc, &v, sizeof(v)); }
void sc_uniform2f(int loc, float a, float b) { float v[2]={a,b}; SCSetUniform(loc,v,sizeof(v)); }
void sc_uniform3f(int loc, float a, float b, float c) { float v[3]={a,b,c}; SCSetUniform(loc,v,sizeof(v)); }
void sc_uniform4f(int loc, float a, float b, float c, float d) { float v[4]={a,b,c,d}; SCSetUniform(loc,v,sizeof(v)); }
void sc_uniform_mat4(int loc, const float *m) { if (m) SCSetUniform(loc,m,16*sizeof(float)); }

static id<MTLBuffer> SCCurrentBuffer(SCBuffer *buffer) {
	if (!buffer) return nil;
	if (gFrameActive && buffer->dynamic && buffer->pageCount[gFrameSlot] > 0)
		return buffer->pages[gFrameSlot][buffer->pageCount[gFrameSlot] - 1];
	return buffer->base;
}

uintptr_t sc_buffer_new(size_t len) {
	if (!gDevice || len == 0) return 0;
	SCBuffer *buffer = calloc(1, sizeof(*buffer));
	if (!buffer) return 0;
	buffer->base = [gDevice newBufferWithLength:len options:MTLResourceStorageModeShared];
	if (!buffer->base) { free(buffer); return 0; }
	buffer->baseSize = len;
	[gBuffers addObject:[NSValue valueWithPointer:buffer]];
	return (uintptr_t)buffer;
}
void sc_buffer_update(uintptr_t h, const void *data, size_t len) {
	if (!h || !data || !len) return;
	SCBuffer *buffer = (SCBuffer *)(void *)h;
	buffer->dynamic = YES;
	if (!gFrameActive) {
		memcpy([buffer->base contents], data, MIN((NSUInteger)len, [buffer->base length]));
		buffer->initialized = YES;
		if (gBoundBuffer == h && gEnc)
			[gEnc setVertexBuffer:buffer->base offset:0 atIndex:0];
		return;
	}
	if (!buffer->initialized) {
		memcpy([buffer->base contents], data, MIN((NSUInteger)len, [buffer->base length]));
		buffer->initialized = YES;
	}
	id<MTLBuffer> target = buffer->base;
	NSUInteger slot = (NSUInteger)gFrameSlot;
	NSUInteger page = buffer->pageCount[slot]++;
	if (page >= buffer->pageCapacity[slot]) {
		NSUInteger capacity = MAX(page + 1, MAX(4, buffer->pageCapacity[slot] * 2));
		id<MTLBuffer> *grown = realloc(buffer->pages[slot], capacity * sizeof(*grown));
		if (!grown) { buffer->pageCount[slot]--; return; }
		memset(grown + buffer->pageCapacity[slot], 0,
		       (capacity - buffer->pageCapacity[slot]) * sizeof(*grown));
		buffer->pages[slot] = grown;
		buffer->pageCapacity[slot] = capacity;
	}
	target = buffer->pages[slot][page];
	if (!target || [target length] < len) {
		[target release];
		target = [gDevice newBufferWithLength:len options:MTLResourceStorageModeShared];
		buffer->pages[slot][page] = target;
	}
	if (!target) { buffer->pageCount[slot]--; return; }
	memcpy([target contents], data, MIN((NSUInteger)len, [target length]));
	if (gBoundBuffer == h && gEnc)
		[gEnc setVertexBuffer:target offset:0 atIndex:0];
}
void sc_buffer_free(uintptr_t h) {
	if (!h) return;
	if (gBoundBuffer == h) gBoundBuffer = 0;
	SCBuffer *buffer = (SCBuffer *)(void *)h;
	[gBuffers removeObject:[NSValue valueWithPointer:buffer]];
	[buffer->base release];
	for (int slot = 0; slot < 3; slot++) {
		for (NSUInteger page = 0; page < buffer->pageCapacity[slot]; page++)
			[buffer->pages[slot][page] release];
		free(buffer->pages[slot]);
	}
	free(buffer);
}
void sc_vertex_buffer(uintptr_t h) {
	gBoundBuffer = h;
	id<MTLBuffer> buffer = SCCurrentBuffer((SCBuffer *)(void *)h);
	if (SCEnsureEncoder() && buffer) [gEnc setVertexBuffer:buffer offset:0 atIndex:0];
}
void sc_draw(int mode, int first, int count) {
	if (!SCEnsureEncoder() || count <= 0) return;
	[gEnc drawPrimitives:mode == 1 ? MTLPrimitiveTypeLine : MTLPrimitiveTypeTriangle
	         vertexStart:(NSUInteger)MAX(first, 0) vertexCount:(NSUInteger)count];
}
static id<MTLSamplerState> SCSampler(BOOL nearest) {
	id<MTLSamplerState> *slot = nearest ? &gNearestSampler : &gLinearSampler;
	if (!*slot && gDevice) {
		MTLSamplerDescriptor *d = [[MTLSamplerDescriptor alloc] init];
		d.minFilter = d.magFilter = nearest ? MTLSamplerMinMagFilterNearest : MTLSamplerMinMagFilterLinear;
		d.sAddressMode = d.tAddressMode = MTLSamplerAddressModeClampToEdge;
		*slot = [gDevice newSamplerStateWithDescriptor:d]; [d release];
	}
	return *slot;
}
uintptr_t sc_texture_new(int w, int h, int nearest) {
	@autoreleasepool {
	if (!gDevice || w <= 0 || h <= 0) return 0;
	MTLTextureDescriptor *d = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA8Unorm width:w height:h mipmapped:NO];
	d.usage = MTLTextureUsageShaderRead; d.storageMode = MTLStorageModeShared;
	id<MTLTexture> t=[gDevice newTextureWithDescriptor:d];
	if (nearest && t) {
		if (!gNearestTextures) gNearestTextures=[[NSMutableSet alloc] init];
		[gNearestTextures addObject:[NSValue valueWithPointer:(void *)t]];
	}
	return (uintptr_t)t;
	}
}
void sc_texture_update(uintptr_t h, const void *pix, int w, int hgt, int row) {
	if (!h || !pix || w <= 0 || hgt <= 0 || row <= 0) return;
	id<MTLTexture> t=(id<MTLTexture>)(void *)h;
	if (w > (int)t.width || hgt > (int)t.height || (int64_t)row < (int64_t)w * 4) return;
	[t replaceRegion:MTLRegionMake2D(0,0,w,hgt) mipmapLevel:0 withBytes:pix bytesPerRow:(NSUInteger)row];
}
void sc_texture_free(uintptr_t h) {
	if (h) {
		if (gBoundTexture == h) gBoundTexture = 0;
		[gNearestTextures removeObject:[NSValue valueWithPointer:(void *)h]];
		[(id<MTLTexture>)(void *)h release];
	}
}
void sc_bind_texture(uintptr_t h) {
	gBoundTexture = h;
	if (!SCEnsureEncoder()) return;
	id<MTLTexture> t = h ? (id<MTLTexture>)(void *)h : nil;
	BOOL nearest = gNearestTextures && [gNearestTextures containsObject:[NSValue valueWithPointer:(void *)h]];
	if (gTargets && [gTargets containsObject:[NSValue valueWithPointer:(void *)h]]) {
		SCTarget *target = (SCTarget *)(void *)h;
		t = target->resolve;
		nearest = target->nearest != 0;
	}
	[gEnc setFragmentTexture:t atIndex:0];
	[gEnc setFragmentSamplerState:SCSampler(nearest) atIndex:0];
}

static void SCFreeTarget(SCTarget *t) {
	if (!t) return; [t->color release]; [t->resolve release]; [t->depth release]; free(t);
}
uintptr_t sc_target_new(int w, int h, int samples, int depth, int nearest) {
	@autoreleasepool {
	if (!gDevice || w <= 0 || h <= 0) return 0;
	SCTarget *t=calloc(1,sizeof(*t)); if (!t) return 0;
	t->width=w; t->height=h; t->samples=SCChooseSampleCount(samples); t->hasDepth=depth != 0; t->nearest=nearest != 0;
	MTLTextureDescriptor *cd=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatBGRA8Unorm width:w height:h mipmapped:NO];
	cd.usage=MTLTextureUsageRenderTarget | MTLTextureUsageShaderRead;
	if (t->samples > 1) {
		cd.textureType=MTLTextureType2DMultisample; cd.sampleCount=(NSUInteger)t->samples; cd.storageMode=MTLStorageModePrivate;
		t->color=[gDevice newTextureWithDescriptor:cd];
		MTLTextureDescriptor *rd=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatBGRA8Unorm width:w height:h mipmapped:NO];
		rd.usage=MTLTextureUsageRenderTarget | MTLTextureUsageShaderRead; rd.storageMode=MTLStorageModeShared;
		t->resolve=[gDevice newTextureWithDescriptor:rd];
	} else {
		cd.storageMode=MTLStorageModeShared; t->color=[gDevice newTextureWithDescriptor:cd]; t->resolve=[t->color retain];
	}
	if (t->hasDepth) {
		MTLTextureDescriptor *dd=[MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatDepth32Float width:w height:h mipmapped:NO];
		dd.usage=MTLTextureUsageRenderTarget; dd.storageMode=MTLStorageModePrivate;
		if (t->samples > 1) { dd.textureType=MTLTextureType2DMultisample; dd.sampleCount=(NSUInteger)t->samples; }
		t->depth=[gDevice newTextureWithDescriptor:dd];
	}
	if (!t->color || !t->resolve || (t->hasDepth && !t->depth)) { SCFreeTarget(t); return 0; }
	if (!gTargets) gTargets=[[NSMutableSet alloc] init];
	[gTargets addObject:[NSValue valueWithPointer:t]];
	return (uintptr_t)t;
	}
}
void sc_target_free(uintptr_t h) {
	if (!h) return;
	[gTargets removeObject:[NSValue valueWithPointer:(void *)h]];
	if (gBoundTexture == h) gBoundTexture = 0;
	if (gCurrentTarget == h) { SCEndEncoder(); gCurrentTarget = 0; }
	SCFreeTarget((SCTarget *)(void *)h);
}
int sc_target_width(uintptr_t h) { SCTarget *t=(SCTarget *)(void *)h; return t ? t->width : 0; }
int sc_target_height(uintptr_t h) { SCTarget *t=(SCTarget *)(void *)h; return t ? t->height : 0; }
int sc_target_max_samples(void) {
	if (!gDevice) return 1;
	for (int n=16; n>=2; n/=2) if ([gDevice supportsTextureSampleCount:(NSUInteger)n]) return n;
	return 1;
}

static void SCEndEncoder(void) {
	if (gEnc) { [gEnc endEncoding]; gEnc = nil; }
}

static int SCChooseSampleCount(int requested) {
	requested = MAX(requested, 1);
	for (int n = 16; n >= 2; n /= 2)
		if (n <= requested && [gDevice supportsTextureSampleCount:(NSUInteger)n]) return n;
	return 1;
}

static MTLRenderPassDescriptor *SCPassDescriptor(int clearColor, int clearDepth) {
	MTLRenderPassDescriptor *rp=[MTLRenderPassDescriptor renderPassDescriptor];
	if (gCurrentTarget == 0) {
		if (!gView || !gDrawable) return nil;
		rp.colorAttachments[0].texture=gDrawable.texture;
		rp.colorAttachments[0].loadAction=clearColor ? MTLLoadActionClear : MTLLoadActionLoad;
		rp.colorAttachments[0].storeAction=MTLStoreActionStore;
		rp.colorAttachments[0].clearColor=MTLClearColorMake(gClear[0],gClear[1],gClear[2],gClear[3]);
		id<MTLTexture> depth=gView.depthStencilTexture;
		if (depth) {
			rp.depthAttachment.texture=depth;
			rp.depthAttachment.loadAction=clearDepth ? MTLLoadActionClear : MTLLoadActionLoad;
			rp.depthAttachment.storeAction=MTLStoreActionStore;
			rp.depthAttachment.clearDepth=1.0;
		}
	} else {
		SCTarget *t=(SCTarget *)(void *)gCurrentTarget;
		if (!t) return nil;
		rp.colorAttachments[0].texture=t->color;
		rp.colorAttachments[0].loadAction=clearColor ? MTLLoadActionClear : MTLLoadActionLoad;
		rp.colorAttachments[0].storeAction=t->samples > 1 ? MTLStoreActionStoreAndMultisampleResolve : MTLStoreActionStore;
		rp.colorAttachments[0].resolveTexture=t->samples > 1 ? t->resolve : nil;
		rp.colorAttachments[0].clearColor=MTLClearColorMake(gClear[0],gClear[1],gClear[2],gClear[3]);
		if (t->depth) {
			rp.depthAttachment.texture=t->depth;
			rp.depthAttachment.loadAction=clearDepth ? MTLLoadActionClear : MTLLoadActionLoad;
			rp.depthAttachment.storeAction=MTLStoreActionStore;
			rp.depthAttachment.clearDepth=1.0;
		}
	}
	return rp;
}

static void SCApplyDepthState(void) {
	if (!gEnc) return;
	MTLDepthStencilDescriptor *d=[[MTLDepthStencilDescriptor alloc] init];
	d.depthCompareFunction = gDepthTest ? (gDepthLEqual ? MTLCompareFunctionLessEqual : MTLCompareFunctionAlways) : MTLCompareFunctionAlways;
	d.depthWriteEnabled = gDepthTest && gDepthWrite;
	id<MTLDepthStencilState> state=[gDevice newDepthStencilStateWithDescriptor:d];
	[gEnc setDepthStencilState:state];
	[state release]; [d release];
	[gEnc setCullMode:gCull ? MTLCullModeBack : MTLCullModeNone];
}

static BOOL SCBeginEncoder(int clearColor, int clearDepth) {
	if (gCurrentTarget == 0 && !gDrawable) return NO;
	if (!gCmdBuf) {
		if (!gQueue) return NO;
		gCmdBuf=[gQueue commandBuffer];
	}
	MTLRenderPassDescriptor *rp=SCPassDescriptor(clearColor,clearDepth);
	if (!rp) return NO;
	gEnc=[gCmdBuf renderCommandEncoderWithDescriptor:rp];
	if (!gEnc) return NO;
	if (gCurrentTarget == 0) {
		gDrawW=(int)gDrawable.texture.width; gDrawH=(int)gDrawable.texture.height;
	} else {
		SCTarget *t=(SCTarget *)(void *)gCurrentTarget; gDrawW=t->width; gDrawH=t->height;
	}
	if (gHasViewport) {
		int x=gLastViewport[0], y=gLastViewport[1], w=gLastViewport[2], h=gLastViewport[3];
		[gEnc setViewport:(MTLViewport){x, gDrawH-(y+h), w, h, 0, 1}];
	} else {
		[gEnc setViewport:(MTLViewport){0,0,gDrawW,gDrawH,0,1}];
	}
	if (gScissorEnabled) SCSetScissorForEncoder();
	else [gEnc setScissorRect:(MTLScissorRect){0,0,(NSUInteger)gDrawW,(NSUInteger)gDrawH}];
	SCApplyDepthState();
	if (gProgram) {
		id<MTLRenderPipelineState> ps=[gProgram pipelineForState];
		if (ps) [gEnc setRenderPipelineState:ps];
		[gProgram bindUniformData];
	}
	if (gBoundBuffer) {
		id<MTLBuffer> buffer = SCCurrentBuffer((SCBuffer *)(void *)gBoundBuffer);
		if (buffer) [gEnc setVertexBuffer:buffer offset:0 atIndex:0];
	}
	if (gBoundTexture) {
		id<MTLTexture> tex=(id<MTLTexture>)(void *)gBoundTexture;
		BOOL nearest=gNearestTextures && [gNearestTextures containsObject:[NSValue valueWithPointer:(void *)gBoundTexture]];
		if ([gTargets containsObject:[NSValue valueWithPointer:(void *)gBoundTexture]]) {
			SCTarget *t=(SCTarget *)(void *)gBoundTexture; tex=t->resolve; nearest=t->nearest != 0;
		}
		[gEnc setFragmentTexture:tex atIndex:0];
		[gEnc setFragmentSamplerState:SCSampler(nearest) atIndex:0];
	}
	return YES;
}

static BOOL SCEnsureEncoder(void) {
	if (gEnc) return YES;
	if (gCurrentTarget == 0 && !gDrawable) return NO;
	return SCBeginEncoder(0,0);
}

int sc_frame_begin(void) {
	if (!gQueue || !gFrameSemaphore || gCmdBuf || gFrameActive) return 0;
	dispatch_semaphore_wait(gFrameSemaphore, DISPATCH_TIME_FOREVER);
	int slot = (gFrameSlot + 1) % 3;
	gDrawable=[gView.currentDrawable retain];
	if (!gDrawable && gVisible) {
		dispatch_semaphore_signal(gFrameSemaphore);
		return 0;
	}
	gCmdBuf=[gQueue commandBuffer];
	if (!gCmdBuf) {
		[gDrawable release]; gDrawable=nil;
		dispatch_semaphore_signal(gFrameSemaphore);
		return 0;
	}
	gFramePermit = YES;
	gFrameSlot = slot;
	for (NSValue *value in gBuffers) {
		SCBuffer *buffer = (SCBuffer *)[value pointerValue];
		buffer->pageCount[gFrameSlot] = 0;
	}
	gFrameActive = YES;
	gCurrentTarget=0; gEnc=nil; gDrawW=gDrawable ? (int)gDrawable.texture.width : 0;
	gDrawH=gDrawable ? (int)gDrawable.texture.height : 0;
	return 1;
}
void sc_frame_end(void) {
	SCEndEncoder();
	if (gCmdBuf) {
		if (gDrawable) [gCmdBuf presentDrawable:gDrawable];
		[gCmdBuf addCompletedHandler:^(id<MTLCommandBuffer> commandBuffer) {
			dispatch_semaphore_signal(gFrameSemaphore);
		}];
		[gCmdBuf commit]; gCmdBuf=nil;
		gFramePermit = NO;
	} else if (gFramePermit) {
		dispatch_semaphore_signal(gFrameSemaphore);
		gFramePermit = NO;
	}
	[gDrawable release]; gDrawable=nil; gCurrentTarget=0; gFrameActive=NO;
}
void sc_bind_target(uintptr_t h) {
	if (gCurrentTarget == h && gEnc) return;
	SCEndEncoder();
	gCurrentTarget=h;
	if (h) {
		SCTarget *t=(SCTarget *)(void *)h;
		gLastViewport[0]=gLastViewport[1]=0;
		gLastViewport[2]=t->width; gLastViewport[3]=t->height;
	} else if (gDrawable) {
		gLastViewport[0]=gLastViewport[1]=0;
		gLastViewport[2]=(int)gDrawable.texture.width; gLastViewport[3]=(int)gDrawable.texture.height;
	}
	gHasViewport=YES;
	SCEnsureEncoder();
}
void sc_target_blit(uintptr_t h) {
	SCTarget *t=(SCTarget *)(void *)h;
	if (!t || !gDrawable || !gCmdBuf) return;
	SCEndEncoder();
	id<MTLBlitCommandEncoder> blit=[gCmdBuf blitCommandEncoder];
	[blit copyFromTexture:t->resolve sourceSlice:0 sourceLevel:0
	          sourceOrigin:MTLOriginMake(0,0,0) sourceSize:MTLSizeMake(t->width,t->height,1)
	             toTexture:gDrawable.texture destinationSlice:0 destinationLevel:0
	        destinationOrigin:MTLOriginMake(0,0,0)];
	[blit endEncoding];
}
void sc_target_read(uintptr_t h, int x, int y, int w, int ht, void *out) {
	SCTarget *t=(SCTarget *)(void *)h;
	if (!t || !out || w<=0 || ht<=0) return;
	BOOL resume = gCmdBuf != nil;
	uintptr_t resumeTarget = gCurrentTarget;
	SCEndEncoder();
	if (gCmdBuf) { [gCmdBuf commit]; [gCmdBuf waitUntilCompleted]; gCmdBuf=nil; }
	else if (gQueue) {
		id<MTLCommandBuffer> sync=[gQueue commandBuffer];
		[sync commit]; [sync waitUntilCompleted];
	}
	int x0=MAX(x,0), y0=MAX(y,0);
	int x1=(int)MIN((int64_t)x + w,t->width), y1=(int)MIN((int64_t)y + ht,t->height);
	if (x1>x0 && y1>y0) {
		char *dst=(char *)out;
		size_t outStride=(size_t)w*4, rw=(size_t)(x1-x0), rh=(size_t)(y1-y0);
		size_t dstOffset=(size_t)(y0-y)*outStride+(size_t)(x0-x)*4;
		[t->resolve getBytes:dst+dstOffset bytesPerRow:outStride
		              fromRegion:MTLRegionMake2D(x0,y0,rw,rh) mipmapLevel:0];
		for (size_t row=0; row<rh; row++) {
			char *line=dst+dstOffset+row*outStride;
			for (size_t col=0; col<rw; col++) {
				char b=line[col*4]; line[col*4]=line[col*4+2]; line[col*4+2]=b;
			}
		}
	}
	if (resume && gQueue) {
		gCmdBuf=[gQueue commandBuffer];
		gCurrentTarget=resumeTarget;
		SCEnsureEncoder();
	}
}
void sc_clear_color(double r, double g, double b, double a) { gClear[0]=r; gClear[1]=g; gClear[2]=b; gClear[3]=a; }
static id<MTLRenderPipelineState> SCClearPipeline(int samples, BOOL hasDepth, BOOL color) {
	if (!gClearPipelines) gClearPipelines=[[NSMutableDictionary alloc] init];
	int key=(samples << 2) | (hasDepth ? 2 : 0) | (color ? 1 : 0);
	NSNumber *cacheKey=[NSNumber numberWithInt:key];
	id<MTLRenderPipelineState> cached=[gClearPipelines objectForKey:cacheKey];
	if (cached) return cached;
	if (!gClearLibrary) {
		NSString *source=@"#include <metal_stdlib>\n"
			"using namespace metal;\n"
			"vertex float4 sc_clear_vs(uint vid [[vertex_id]]) {\n"
			"  float2 p = vid == 0 ? float2(-1,-1) : (vid == 1 ? float2(3,-1) : float2(-1,3));\n"
			"  return float4(p,1,1);\n"
			"}\n"
			"fragment float4 sc_clear_fs(constant float4 &color [[buffer(0)]]) { return color; }\n";
		NSError *error=nil;
		gClearLibrary=[gDevice newLibraryWithSource:source options:nil error:&error];
		if (!gClearLibrary) { SCLogError("Metal clear shader compilation failed", error); return nil; }
	}
	id<MTLFunction> vertex=[gClearLibrary newFunctionWithName:@"sc_clear_vs"];
	id<MTLFunction> fragment=[gClearLibrary newFunctionWithName:@"sc_clear_fs"];
	if (!vertex || !fragment) { [vertex release]; [fragment release]; return nil; }
	MTLRenderPipelineDescriptor *descriptor=[[MTLRenderPipelineDescriptor alloc] init];
	descriptor.vertexFunction=vertex; descriptor.fragmentFunction=fragment;
	descriptor.colorAttachments[0].pixelFormat=MTLPixelFormatBGRA8Unorm;
	descriptor.colorAttachments[0].writeMask=color ? MTLColorWriteMaskAll : MTLColorWriteMaskNone;
	descriptor.depthAttachmentPixelFormat=hasDepth ? MTLPixelFormatDepth32Float : MTLPixelFormatInvalid;
	descriptor.rasterSampleCount=(NSUInteger)samples;
	NSError *error=nil;
	id<MTLRenderPipelineState> pipeline=[gDevice newRenderPipelineStateWithDescriptor:descriptor error:&error];
	if (!pipeline) SCLogError("Metal clear pipeline creation failed", error);
	else [gClearPipelines setObject:pipeline forKey:cacheKey];
	[pipeline release]; [descriptor release]; [vertex release]; [fragment release];
	return [gClearPipelines objectForKey:cacheKey];
}
void sc_clear(int color, int depth) {
	if (gEnc) SCEndEncoder();
	if (!color && !depth) return;
	if (gScissorEnabled) {
		if (!SCBeginEncoder(0,0)) return;
		BOOL hasDepth=gCurrentTarget == 0
			? (gView.depthStencilTexture != nil)
			: (((SCTarget *)(void *)gCurrentTarget)->depth != nil);
		BOOL clearDepth=depth && hasDepth;
		if (!color && !clearDepth) { SCEndEncoder(); return; }
		int samples=gCurrentTarget == 0 ? (int)gView.sampleCount : ((SCTarget *)(void *)gCurrentTarget)->samples;
		id<MTLRenderPipelineState> pipeline=SCClearPipeline(samples,hasDepth,color != 0);
		if (!pipeline) { SCEndEncoder(); return; }
		[gEnc setViewport:(MTLViewport){0,0,gDrawW,gDrawH,0,1}];
		[gEnc setScissorRect:gScissorRect];
		[gEnc setRenderPipelineState:pipeline];
		[gEnc setCullMode:MTLCullModeNone];
		MTLDepthStencilDescriptor *depthDescriptor=[[MTLDepthStencilDescriptor alloc] init];
		depthDescriptor.depthCompareFunction=MTLCompareFunctionAlways;
		depthDescriptor.depthWriteEnabled=clearDepth;
		id<MTLDepthStencilState> depthState=[gDevice newDepthStencilStateWithDescriptor:depthDescriptor];
		[gEnc setDepthStencilState:depthState];
		[depthState release]; [depthDescriptor release];
		float clear[4]={(float)gClear[0],(float)gClear[1],(float)gClear[2],(float)gClear[3]};
		[gEnc setFragmentBytes:clear length:sizeof(clear) atIndex:0];
		[gEnc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:3];
		SCEndEncoder();
		return;
	}
	if (SCBeginEncoder(color,depth)) {
		SCEndEncoder();
	}
}
void sc_viewport(int x, int y, int w, int h) {
	gLastViewport[0]=x; gLastViewport[1]=y; gLastViewport[2]=MAX(w,0); gLastViewport[3]=MAX(h,0); gHasViewport=YES;
	if (!SCEnsureEncoder()) return;
	int yy=gDrawH-(y+h); if (yy<0) yy=0;
	[gEnc setViewport:(MTLViewport){x,yy,MAX(w,0),MAX(h,0),0,1}];
}
static void SCSetScissorForEncoder(void) {
	int64_t x0 = MAX((int64_t)gScissorArgs[0], 0);
	int64_t y0 = MAX((int64_t)gScissorArgs[1], 0);
	int64_t x1 = MIN((int64_t)gScissorArgs[0] + MAX(gScissorArgs[2], 0), gDrawW);
	int64_t y1 = MIN((int64_t)gScissorArgs[1] + MAX(gScissorArgs[3], 0), gDrawH);
	x0 = MIN(x0, gDrawW); y0 = MIN(y0, gDrawH);
	if (x1 < x0) x1 = x0;
	if (y1 < y0) y1 = y0;
	NSUInteger width = (NSUInteger)(x1 - x0), height = (NSUInteger)(y1 - y0);
	NSUInteger y = (NSUInteger)(gDrawH - y1);
	gScissorRect = (MTLScissorRect){(NSUInteger)x0, y, width, height};
	if (gEnc) [gEnc setScissorRect:gScissorRect];
}
void sc_scissor(int x, int y, int w, int h) {
	gScissorArgs[0]=x; gScissorArgs[1]=y; gScissorArgs[2]=w; gScissorArgs[3]=h;
	gScissorEnabled=YES;
	if (SCEnsureEncoder()) SCSetScissorForEncoder();
}
void sc_scissor_reset(void) {
	gScissorEnabled=NO;
	if (SCEnsureEncoder()) [gEnc setScissorRect:(MTLScissorRect){0,0,(NSUInteger)gDrawW,(NSUInteger)gDrawH}];
}
void sc_set_depth_test(int on) { gDepthTest=on!=0; SCApplyDepthState(); }
void sc_set_depth_write(int on) { gDepthWrite=on!=0; SCApplyDepthState(); }
void sc_set_depth_func_lequal(int on) { gDepthLEqual=on!=0; SCApplyDepthState(); }
void sc_set_blend(int on) {
	gBlend=on!=0;
	if (gProgram && gEnc) { id<MTLRenderPipelineState> ps=[gProgram pipelineForState]; if(ps)[gEnc setRenderPipelineState:ps]; }
}
void sc_set_cull(int on) { gCull=on!=0; if(gEnc)[gEnc setCullMode:gCull?MTLCullModeBack:MTLCullModeNone]; }
