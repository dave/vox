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

static void postKey(CGEventSourceRef src, CGKeyCode k, bool down, CGEventFlags f) {
    CGEventRef e = CGEventCreateKeyboardEvent(src, k, down);
    CGEventSetFlags(e, f);
    CGEventPost(kCGHIDEventTap, e);
    CFRelease(e);
    usleep(8000); // some apps (Electron, browsers) drop keys that arrive in the same instant
}

// Full Cmd-down, V-down, V-up, Cmd-up sequence from a private source, so the
// physical modifier state (e.g. Fn still settling) doesn't leak into it.
static void postCmdV(void) {
    CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStatePrivate);
    postKey(src, 55 /* kVK_Command */, true, kCGEventFlagMaskCommand);
    postKey(src, 9 /* kVK_ANSI_V */, true, kCGEventFlagMaskCommand);
    postKey(src, 9, false, kCGEventFlagMaskCommand);
    postKey(src, 55, false, 0);
    CFRelease(src);
}

void dw_paste(const char *text, int restore_delay_ms) {
    NSString *str = [NSString stringWithUTF8String:text];
    dispatch_async(dispatch_get_main_queue(), ^{
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSMutableArray *saved = [NSMutableArray array];
        if (restore_delay_ms > 0) {
            for (NSPasteboardItem *it in pb.pasteboardItems) {
                NSPasteboardItem *c = [NSPasteboardItem new];
                for (NSString *t in it.types) {
                    NSData *data = [it dataForType:t];
                    if (data) [c setData:data forType:t];
                }
                [saved addObject:c];
            }
        }
        [pb clearContents];
        [pb setString:str forType:NSPasteboardTypeString];
        NSInteger cc = pb.changeCount;
        NSString *app = NSWorkspace.sharedWorkspace.frontmostApplication.localizedName ?: @"?";
        goPasteLog((char *)app.UTF8String, (int)saved.count);
        // Let the pasteboard change propagate before the target app reads it.
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 40 * NSEC_PER_MSEC),
                       dispatch_get_global_queue(QOS_CLASS_USER_INTERACTIVE, 0), ^{
            postCmdV();
            if (restore_delay_ms <= 0) return;
            dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)restore_delay_ms * NSEC_PER_MSEC),
                           dispatch_get_main_queue(), ^{
                if (pb.changeCount != cc) return; // someone else copied meanwhile
                [pb clearContents];
                if (saved.count) [pb writeObjects:saved];
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
