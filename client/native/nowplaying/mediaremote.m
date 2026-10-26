// Read-only MediaRemote bridge, hosted by system Perl as in MediaRemoteAdapter.
// This is a project-owned implementation; no boring.notch code is bundled.
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#include <dlfcn.h>
#include <math.h>
#include <stdio.h>

static void (*getInfo)(dispatch_queue_t, void (^)(NSDictionary *));
static void (*getIsPlaying)(dispatch_queue_t, void (^)(bool));
static void (*getPID)(dispatch_queue_t, void (^)(int));
static BOOL reading = NO;
static NSUInteger generation = 0;
static NSMutableArray *observers;

static void emit(NSDictionary *current, NSString *error) {
    NSDictionary *message = @{ @"current": current ?: (id)[NSNull null],
                              @"error_code": error ?: (id)[NSNull null] };
    NSData *data = [NSJSONSerialization dataWithJSONObject:message options:0 error:nil];
    if (!data) { fputs("{\"current\":null,\"error_code\":\"music_protocol_error\"}\n", stdout); }
    else { fwrite(data.bytes, 1, data.length, stdout); fputc('\n', stdout); }
    fflush(stdout);
}

static id text(id value) { return [value isKindOfClass:NSString.class] ? value : [NSNull null]; }
static id seconds(id value) {
    if (![value isKindOfClass:NSNumber.class]) return [NSNull null];
    double d = [value doubleValue];
    return isfinite(d) && d >= 0 ? @(d) : (id)[NSNull null];
}

static void readCurrent(void) {
    if (reading) return;
    reading = YES;
    NSUInteger request = ++generation;
    dispatch_queue_t queue = dispatch_get_main_queue();
    dispatch_group_t group = dispatch_group_create();
    __block NSDictionary *info = nil;
    __block int pid = 0;
    __block bool isPlaying = false;
    dispatch_group_enter(group);
    getInfo(queue, ^(NSDictionary *value) {
        if ([value isKindOfClass:NSDictionary.class]) info = value;
        dispatch_group_leave(group);
    });
    dispatch_group_enter(group);
    getIsPlaying(queue, ^(bool value) { isPlaying = value; dispatch_group_leave(group); });
    if (getPID) {
        dispatch_group_enter(group);
        getPID(queue, ^(int value) { pid = value; dispatch_group_leave(group); });
    }
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 5*NSEC_PER_SEC), queue, ^{
        if (reading && generation == request) {
            reading = NO;
            ++generation;
            emit(nil, @"music_timeout");
        }
    });
    dispatch_group_notify(group, queue, ^{
        if (generation != request) return;
        reading = NO;
        if (info.count == 0 && !isPlaying) { emit(nil, nil); return; }
        NSMutableDictionary *current = [NSMutableDictionary dictionary];
        current[@"title"] = text(info[@"kMRMediaRemoteNowPlayingInfoTitle"]);
        current[@"artist"] = text(info[@"kMRMediaRemoteNowPlayingInfoArtist"]);
        NSRunningApplication *app = pid > 0 ? [NSRunningApplication runningApplicationWithProcessIdentifier:pid] : nil;
        current[@"source"] = text(app.localizedName);
        // System boolean is authoritative; rate is used only to interpolate time.
        id rate = info[@"kMRMediaRemoteNowPlayingInfoPlaybackRate"];
        current[@"state"] = isPlaying ? @"playing" : @"not_playing";
        current[@"duration"] = seconds(info[@"kMRMediaRemoteNowPlayingInfoDuration"]);
        id elapsed = seconds(info[@"kMRMediaRemoteNowPlayingInfoElapsedTime"]);
        NSDate *stamp = info[@"kMRMediaRemoteNowPlayingInfoTimestamp"];
        if ([elapsed isKindOfClass:NSNumber.class] && [stamp isKindOfClass:NSDate.class] && [rate isKindOfClass:NSNumber.class]) {
            double delta = -stamp.timeIntervalSinceNow;
            if (isPlaying && delta >= 0) elapsed = @(MAX(0, [elapsed doubleValue] + delta * MAX(0, [rate doubleValue])));
        }
        current[@"position"] = elapsed;
        emit(current, nil);
    });
}

// DynaLoader invokes this as a Perl XSUB. We never return into Perl and do not
// touch its interpreter state, so no Perl headers or ABI-specific structures.
__attribute__((visibility("default"))) void monitor_media_run(void *interpreter, void *cv) {
    @autoreleasepool {
        void *framework = dlopen("/System/Library/PrivateFrameworks/MediaRemote.framework/MediaRemote", RTLD_LAZY|RTLD_LOCAL);
        if (!framework) { emit(nil, @"music_framework_missing"); exit(1); }
        getInfo = dlsym(framework, "MRMediaRemoteGetNowPlayingInfo");
        getPID = dlsym(framework, "MRMediaRemoteGetNowPlayingApplicationPID");
        getIsPlaying = dlsym(framework, "MRMediaRemoteGetNowPlayingApplicationIsPlaying");
        if (!getInfo || !getIsPlaying) { emit(nil, @"music_symbols_missing"); exit(1); }
        void (*registerNotifications)(dispatch_queue_t) = dlsym(framework, "MRMediaRemoteRegisterForNowPlayingNotifications");
        if (registerNotifications) {
            registerNotifications(dispatch_get_main_queue());
            observers = [NSMutableArray array];
            for (NSString *name in @[@"kMRMediaRemoteNowPlayingInfoDidChangeNotification",
                                     @"kMRMediaRemoteNowPlayingApplicationDidChangeNotification",
                                     @"kMRMediaRemoteNowPlayingApplicationIsPlayingDidChangeNotification"]) {
                id observer = [NSNotificationCenter.defaultCenter addObserverForName:name object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) { readCurrent(); }];
                [observers addObject:observer];
            }
        }
        // Heartbeat complete reads distinguish an unchanged track from a dead
        // stream and work as polling fallback if registration is unavailable.
        [NSTimer scheduledTimerWithTimeInterval:2 repeats:YES block:^(NSTimer *timer) { readCurrent(); }];
        readCurrent();
        [NSRunLoop.currentRunLoop run];
    }
}
