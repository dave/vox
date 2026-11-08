#import <Cocoa/Cocoa.h>
#import <AudioToolbox/AudioToolbox.h>
#import <ApplicationServices/ApplicationServices.h>
#include <unistd.h>
#include "mac.h"
#include "_cgo_export.h"

// ---------- permissions ----------

int dw_check_perms(int prompt) {
    int r = 0;
    if (CGPreflightListenEventAccess()) r |= 1;
    else if (prompt) CGRequestListenEventAccess();
    if (CGPreflightPostEventAccess()) r |= 2;
    else if (prompt) CGRequestPostEventAccess();
    return r;
}

// ---------- Fn key event tap (listen-only: never swallows events) ----------

static CFMachPortRef gTap;
static int gFnDown;

static CGEventRef tapCB(CGEventTapProxy proxy, CGEventType type, CGEventRef e, void *u) {
    if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
        CGEventTapEnable(gTap, true);
        return e;
    }
    if (type == kCGEventFlagsChanged) {
        int down = (CGEventGetFlags(e) & kCGEventFlagMaskSecondaryFn) != 0;
        int64_t kc = CGEventGetIntegerValueField(e, kCGKeyboardEventKeycode);
        if (down != gFnDown && (kc == 63 || kc == 179)) {
            gFnDown = down;
            goFnEvent(down);
        } else if (gFnDown && kc != 63 && kc != 179) {
            goOtherKey(); // another modifier while Fn held
        }
    } else if (type == kCGEventKeyDown) {
        if (gFnDown) goOtherKey(); // Fn+key shortcut, not dictation
    }
    return e;
}

int dw_start_tap(void) {
    CGEventMask m = CGEventMaskBit(kCGEventFlagsChanged) | CGEventMaskBit(kCGEventKeyDown);
    gTap = CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap,
                            kCGEventTapOptionListenOnly, m, tapCB, NULL);
    if (!gTap) return -1;
    CFRunLoopSourceRef src = CFMachPortCreateRunLoopSource(NULL, gTap, 0);
    CFRunLoopAddSource(CFRunLoopGetMain(), src, kCFRunLoopCommonModes);
    CGEventTapEnable(gTap, true);
    return 0;
}

void dw_run_loop(void) {
    [NSApplication sharedApplication];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
    CFRunLoopRun();
}

// ---------- audio capture (AudioQueue, 16 kHz mono int16) ----------

#define NBUF 4
#define BUFBYTES 1600 /* 50 ms */
static AudioQueueRef gQ;
static volatile int gRec;

static void aqCB(void *u, AudioQueueRef q, AudioQueueBufferRef b, const AudioTimeStamp *t,
                 UInt32 n, const AudioStreamPacketDescription *d) {
    if (b->mAudioDataByteSize > 0) goAudio(b->mAudioData, (int)b->mAudioDataByteSize);
    if (gRec) AudioQueueEnqueueBuffer(q, b, 0, NULL);
}

int dw_audio_start(void) {
    AudioStreamBasicDescription f = {0};
    f.mSampleRate = 16000;
    f.mFormatID = kAudioFormatLinearPCM;
    f.mFormatFlags = kLinearPCMFormatFlagIsSignedInteger | kLinearPCMFormatFlagIsPacked;
    f.mChannelsPerFrame = 1;
    f.mBitsPerChannel = 16;
    f.mBytesPerFrame = 2;
    f.mFramesPerPacket = 1;
    f.mBytesPerPacket = 2;
    OSStatus s = AudioQueueNewInput(&f, aqCB, NULL, NULL, NULL, 0, &gQ);
    if (s) return (int)s;
    for (int i = 0; i < NBUF; i++) {
        AudioQueueBufferRef b;
        AudioQueueAllocateBuffer(gQ, BUFBYTES, &b);
        AudioQueueEnqueueBuffer(gQ, b, 0, NULL);
    }
    gRec = 1;
    s = AudioQueueStart(gQ, NULL);
    if (s) { gRec = 0; AudioQueueDispose(gQ, true); gQ = NULL; }
    return (int)s;
}

void dw_audio_stop(void) {
    if (!gQ) return;
    gRec = 0;
    AudioQueueStop(gQ, true);
    AudioQueueDispose(gQ, true);
    gQ = NULL;
}

// ---------- paste via clipboard + Cmd-V ----------

// Same as TypeWhisper (which never dropped pastes): no event source, plain
// V down/up with the Command flag, posted at the session level.
static void postCmdV(void) {
    CGEventRef d = CGEventCreateKeyboardEvent(NULL, 9 /* kVK_ANSI_V */, true);
    CGEventRef u = CGEventCreateKeyboardEvent(NULL, 9, false);
    CGEventSetFlags(d, kCGEventFlagMaskCommand);
    CGEventSetFlags(u, kCGEventFlagMaskCommand);
    CGEventPost(kCGSessionEventTap, d);
    CGEventPost(kCGSessionEventTap, u);
    CFRelease(d); CFRelease(u);
}

static void plog(NSString *fmt, ...) NS_FORMAT_FUNCTION(1,2);
static void plog(NSString *fmt, ...) {
    va_list ap; va_start(ap, fmt);
    NSString *m = [[NSString alloc] initWithFormat:fmt arguments:ap];
    va_end(ap);
    goLog((char *)m.UTF8String);
}

// Describes the focused UI element (via Accessibility): role and owning app.
static NSString *focusInfo(void) {
    AXUIElementRef sys = AXUIElementCreateSystemWide();
    CFTypeRef el = NULL;
    AXError err = AXUIElementCopyAttributeValue(sys, kAXFocusedUIElementAttribute, &el);
    CFRelease(sys);
    if (err != kAXErrorSuccess || !el) return [NSString stringWithFormat:@"focus=? (AX error %d)", (int)err];
    CFTypeRef role = NULL;
    AXUIElementCopyAttributeValue(el, kAXRoleAttribute, &role);
    pid_t pid = 0;
    AXUIElementGetPid(el, &pid);
    NSString *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid].localizedName ?: @"?";
    NSString *r = [NSString stringWithFormat:@"focus=%@ in %@", role ? (__bridge NSString *)role : @"?", app];
    if (role) CFRelease(role);
    CFRelease(el);
    return r;
}

static NSString *modsInfo(void) {
    CGEventFlags f = CGEventSourceFlagsState(kCGEventSourceStateHIDSystemState);
    NSMutableArray *m = [NSMutableArray array];
    if (f & kCGEventFlagMaskSecondaryFn) [m addObject:@"fn"];
    if (f & kCGEventFlagMaskCommand) [m addObject:@"cmd"];
    if (f & kCGEventFlagMaskShift) [m addObject:@"shift"];
    if (f & kCGEventFlagMaskAlternate) [m addObject:@"opt"];
    if (f & kCGEventFlagMaskControl) [m addObject:@"ctrl"];
    return m.count ? [m componentsJoinedByString:@"+"] : @"none";
}

// Supplies the dictated text lazily, so we can log when (and whether) an app
// actually reads the clipboard. A read shortly after Cmd-V means the paste landed.
@interface VoxProvider : NSObject <NSPasteboardItemDataProvider>
@property (copy) NSString *text;
@property CFAbsoluteTime posted;
@property int reads;
@end
@implementation VoxProvider
- (void)pasteboard:(NSPasteboard *)pb item:(NSPasteboardItem *)item provideDataForType:(NSPasteboardType)type {
    self.reads++;
    double ms = self.posted ? (CFAbsoluteTimeGetCurrent() - self.posted) * 1000 : -1;
    plog(@"paste: clipboard read #%d (%@) %.0fms after Cmd-V; frontmost=%@", self.reads, type, ms,
         NSWorkspace.sharedWorkspace.frontmostApplication.localizedName);
    [item setString:self.text forType:type];
}
- (void)pasteboardFinishedWithDataProvider:(NSPasteboard *)pb {}
@end

static VoxProvider *provider; // keep alive while it owns the pasteboard

void dw_paste(const char *text, int restore_delay_ms) {
    NSString *str = [NSString stringWithUTF8String:text];
    dispatch_async(dispatch_get_main_queue(), ^{
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSMutableArray *saved = [NSMutableArray array];
        if (restore_delay_ms > 0) {
            for (NSPasteboardItem *it in pb.pasteboardItems) {
                NSPasteboardItem *c = [NSPasteboardItem new];
                NSMutableArray *missing = [NSMutableArray array];
                for (NSString *t in it.types) {
                    NSData *data = [it dataForType:t];
                    if (data) [c setData:data forType:t];
                    else [missing addObject:t];
                }
                if (missing.count) plog(@"paste: couldn't save clipboard types %@", missing);
                [saved addObject:c];
            }
        }
        provider = [VoxProvider new];
        provider.text = str;
        NSPasteboardItem *item = [NSPasteboardItem new];
        [item setDataProvider:provider forTypes:@[NSPasteboardTypeString]];
        [pb clearContents];
        BOOL ok = [pb writeObjects:@[item]];
        NSInteger cc = pb.changeCount;
        NSString *app = NSWorkspace.sharedWorkspace.frontmostApplication.localizedName ?: @"?";
        plog(@"paste: clipboard set (ok=%d, change %ld), saved %lu old item(s); frontmost=%@, %@, mods=%@",
             ok, (long)cc, (unsigned long)saved.count, app, focusInfo(), modsInfo());
        VoxProvider *p = provider;
        // Let the pasteboard change propagate before the target app reads it.
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 40 * NSEC_PER_MSEC),
                       dispatch_get_global_queue(QOS_CLASS_USER_INTERACTIVE, 0), ^{
            plog(@"paste: posting Cmd-V; frontmost=%@, mods=%@",
                 NSWorkspace.sharedWorkspace.frontmostApplication.localizedName, modsInfo());
            p.posted = CFAbsoluteTimeGetCurrent();
            postCmdV();
            int delay = restore_delay_ms > 0 ? restore_delay_ms : 1500;
            dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)delay * NSEC_PER_MSEC),
                           dispatch_get_main_queue(), ^{
                if (p.reads == 0)
                    plog(@"paste: WARNING nothing read the clipboard within %dms of Cmd-V — the paste probably didn't land", delay);
                if (restore_delay_ms <= 0) {
                    // Replace the lazy promise with real data, so the text stays
                    // on the clipboard even after vox quits.
                    if (pb.changeCount == cc) {
                        [pb clearContents];
                        [pb setString:p.text forType:NSPasteboardTypeString];
                    }
                    return;
                }
                if (pb.changeCount != cc) {
                    plog(@"paste: clipboard changed by someone else, not restoring");
                    return;
                }
                [pb clearContents];
                BOOL rok = saved.count ? [pb writeObjects:saved] : YES;
                plog(@"paste: restored previous clipboard (ok=%d)", rok);
            });
        });
    });
}

// In-process sound playback with a cache: much lower latency than spawning afplay.
static NSMutableDictionary<NSString *, NSSound *> *sounds;

static NSSound *dw_sound(NSString *p) {
    if (!sounds) sounds = [NSMutableDictionary new];
    NSSound *s = sounds[p];
    if (!s) {
        s = [[NSSound alloc] initWithContentsOfFile:p byReference:YES];
        if (s) sounds[p] = s;
    }
    return s;
}

void dw_play_sound(const char *path, float volume, int preload_only) {
    NSString *p = [NSString stringWithUTF8String:path];
    dispatch_async(dispatch_get_main_queue(), ^{
        NSSound *s = dw_sound(p);
        if (!s || preload_only) return;
        [s stop];
        s.volume = volume;
        [s play];
    });
}
