// syscapture — native macOS system-audio capture for the Sales Copilot client.
//
// It taps everything the machine is playing (the prospect's voice arriving through
// Zoom/Meet) using the CoreAudio process-tap API (macOS 14.4+) — NO driver install,
// user-space only. Loaded in the Electron MAIN process (Node context); the renderer
// runs with nodeIntegration disabled and cannot load native modules itself.
//
// Exposed to JS:
//   start(onAudio): { sampleRate }   // onAudio(Float32Array) — mono samples
//   stop(): void
//
// See techdocs/audio_capture_techdoc.md §19.

#include <napi.h>
#import <Foundation/Foundation.h>
#import <CoreAudio/CoreAudio.h>
#if __has_include(<CoreAudio/CATapDescription.h>)
#import <CoreAudio/CATapDescription.h>
#import <CoreAudio/AudioHardwareTapping.h>
#endif

#include <cstdio>
#include <cstring>
#include <vector>

// One capture session at a time (one live call).
static AudioObjectID gTapID = kAudioObjectUnknown;
static AudioObjectID gAggDevID = kAudioObjectUnknown;
static AudioDeviceIOProcID gIOProcID = nullptr;
static Napi::ThreadSafeFunction gTSFN;
static bool gRunning = false;

// Tear down whatever has been created. Safe to call repeatedly / partially set up.
static void StopInternal() {
  if (gAggDevID != kAudioObjectUnknown && gIOProcID) {
    AudioDeviceStop(gAggDevID, gIOProcID);
    AudioDeviceDestroyIOProcID(gAggDevID, gIOProcID);
  }
  gIOProcID = nullptr;
  if (gAggDevID != kAudioObjectUnknown) {
    AudioHardwareDestroyAggregateDevice(gAggDevID);
    gAggDevID = kAudioObjectUnknown;
  }
  if (gTapID != kAudioObjectUnknown) {
    AudioHardwareDestroyProcessTap(gTapID);
    gTapID = kAudioObjectUnknown;
  }
  if (gTSFN) {
    gTSFN.Release();
    gTSFN = Napi::ThreadSafeFunction();
  }
  gRunning = false;
}

// Throw a JS error and return true when an OSStatus indicates failure.
static bool failIf(Napi::Env env, OSStatus st, const char* what) {
  if (st != noErr) {
    char msg[256];
    snprintf(msg, sizeof(msg), "syscapture: %s failed (OSStatus %d)", what, (int)st);
    Napi::Error::New(env, msg).ThrowAsJavaScriptException();
    return true;
  }
  return false;
}

// Audio IO callback. Runs on a real-time CoreAudio thread — NOT the JS thread — so we
// copy + downmix to mono here, then hand the samples to JS via the thread-safe
// function (TSFN), which marshals them onto the JS thread.
static OSStatus TapIOProc(AudioObjectID inDevice, const AudioTimeStamp* inNow,
                          const AudioBufferList* inInputData,
                          const AudioTimeStamp* inInputTime,
                          AudioBufferList* outOutputData,
                          const AudioTimeStamp* inOutputTime, void* inClientData) {
  (void)inDevice; (void)inNow; (void)inInputTime;
  (void)outOutputData; (void)inOutputTime; (void)inClientData;
  if (!inInputData || inInputData->mNumberBuffers == 0) return noErr;

  std::vector<float>* mono = nullptr;
  const UInt32 nbuf = inInputData->mNumberBuffers;

  if (nbuf >= 2) {
    // Non-interleaved: one buffer per channel. Average across channels.
    const AudioBuffer& b0 = inInputData->mBuffers[0];
    const size_t frames = b0.mDataByteSize / sizeof(float);
    mono = new std::vector<float>(frames, 0.0f);
    UInt32 used = 0;
    for (UInt32 ch = 0; ch < nbuf; ch++) {
      const AudioBuffer& b = inInputData->mBuffers[ch];
      const float* s = (const float*)b.mData;
      if (!s || (b.mDataByteSize / sizeof(float)) != frames) continue;
      for (size_t i = 0; i < frames; i++) (*mono)[i] += s[i];
      used++;
    }
    if (used > 1) {
      for (size_t i = 0; i < frames; i++) (*mono)[i] /= (float)used;
    }
  } else {
    // Single buffer: mono, or interleaved stereo.
    const AudioBuffer& b = inInputData->mBuffers[0];
    const float* s = (const float*)b.mData;
    if (!s) return noErr;
    const UInt32 ch = b.mNumberChannels > 0 ? b.mNumberChannels : 1;
    const size_t frames = (b.mDataByteSize / sizeof(float)) / ch;
    mono = new std::vector<float>(frames, 0.0f);
    for (size_t f = 0; f < frames; f++) {
      float sum = 0.0f;
      for (UInt32 c = 0; c < ch; c++) sum += s[f * ch + c];
      (*mono)[f] = sum / (float)ch;
    }
  }

  if (!mono) return noErr;

  napi_status status = gTSFN.NonBlockingCall(
      mono, [](Napi::Env env, Napi::Function cb, std::vector<float>* data) {
        Napi::Float32Array arr = Napi::Float32Array::New(env, data->size());
        memcpy(arr.Data(), data->data(), data->size() * sizeof(float));
        cb.Call({arr});
        delete data;
      });
  if (status != napi_ok) {
    delete mono;  // TSFN closing/full — drop this chunk rather than leak.
  }
  return noErr;
}

Napi::Value Start(const Napi::CallbackInfo& info) {
  Napi::Env env = info.Env();
  if (gRunning) {
    Napi::Error::New(env, "syscapture: already running").ThrowAsJavaScriptException();
    return env.Undefined();
  }
  if (info.Length() < 1 || !info[0].IsFunction()) {
    Napi::TypeError::New(env, "syscapture.start(callback) requires a function")
        .ThrowAsJavaScriptException();
    return env.Undefined();
  }

#if __has_include(<CoreAudio/CATapDescription.h>)
  // 1) Describe a tap of all system output (exclude no processes).
  CATapDescription* desc =
      [[CATapDescription alloc] initStereoGlobalTapButExcludeProcesses:@[]];
  desc.name = @"SalesCopilotSystemTap";
  desc.muteBehavior = CATapUnmuted;  // capture without muting the user's own audio

  // 2) Create the process tap.
  OSStatus st = AudioHardwareCreateProcessTap(desc, &gTapID);
  if (failIf(env, st, "AudioHardwareCreateProcessTap")) { StopInternal(); return env.Undefined(); }

  // 3) Read the tap's UID and audio format (the format gives us the sample rate).
  CFStringRef tapUID = NULL;
  UInt32 sz = sizeof(tapUID);
  AudioObjectPropertyAddress uidAddr = {
      kAudioTapPropertyUID, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
  st = AudioObjectGetPropertyData(gTapID, &uidAddr, 0, NULL, &sz, &tapUID);
  if (failIf(env, st, "get tap UID")) { StopInternal(); return env.Undefined(); }

  AudioStreamBasicDescription asbd;
  memset(&asbd, 0, sizeof(asbd));
  UInt32 fsz = sizeof(asbd);
  AudioObjectPropertyAddress fmtAddr = {
      kAudioTapPropertyFormat, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
  st = AudioObjectGetPropertyData(gTapID, &fmtAddr, 0, NULL, &fsz, &asbd);
  if (failIf(env, st, "get tap format")) {
    if (tapUID) CFRelease(tapUID);
    StopInternal();
    return env.Undefined();
  }
  const double sampleRate = asbd.mSampleRate;

  // 4) Create a private aggregate device that includes the tap.
  NSString* aggUID = [[NSUUID UUID] UUIDString];
  NSDictionary* tapItem = @{@(kAudioSubTapUIDKey) : (__bridge NSString*)tapUID};
  NSDictionary* aggDesc = @{
    @(kAudioAggregateDeviceNameKey) : @"SalesCopilotAggregate",
    @(kAudioAggregateDeviceUIDKey) : aggUID,
    @(kAudioAggregateDeviceIsPrivateKey) : @YES,
    @(kAudioAggregateDeviceTapListKey) : @[ tapItem ]
  };
  st = AudioHardwareCreateAggregateDevice((__bridge CFDictionaryRef)aggDesc, &gAggDevID);
  if (tapUID) CFRelease(tapUID);
  if (failIf(env, st, "AudioHardwareCreateAggregateDevice")) { StopInternal(); return env.Undefined(); }

  // 5) Bridge to JS, then install + start the IO callback.
  gTSFN = Napi::ThreadSafeFunction::New(env, info[0].As<Napi::Function>(),
                                        "syscaptureCallback", 0, 1);

  st = AudioDeviceCreateIOProcID(gAggDevID, TapIOProc, NULL, &gIOProcID);
  if (failIf(env, st, "AudioDeviceCreateIOProcID")) { StopInternal(); return env.Undefined(); }

  st = AudioDeviceStart(gAggDevID, gIOProcID);
  if (failIf(env, st, "AudioDeviceStart")) { StopInternal(); return env.Undefined(); }

  gRunning = true;

  Napi::Object result = Napi::Object::New(env);
  result.Set("sampleRate", Napi::Number::New(env, sampleRate));
  return result;
#else
  Napi::Error::New(env, "syscapture: built without the macOS 14.4+ CoreAudio tap SDK")
      .ThrowAsJavaScriptException();
  return env.Undefined();
#endif
}

Napi::Value Stop(const Napi::CallbackInfo& info) {
  StopInternal();
  return info.Env().Undefined();
}

Napi::Object Init(Napi::Env env, Napi::Object exports) {
  exports.Set("start", Napi::Function::New(env, Start));
  exports.Set("stop", Napi::Function::New(env, Stop));
  return exports;
}

NODE_API_MODULE(syscapture, Init)
