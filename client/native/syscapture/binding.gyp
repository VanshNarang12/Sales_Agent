{
  "targets": [
    {
      "target_name": "syscapture",
      "sources": [ "src/addon.mm" ],
      "include_dirs": [
        "<!@(node -p \"require('node-addon-api').include_dir\")"
      ],
      "conditions": [
        [ "OS=='mac'", {
          "xcode_settings": {
            "CLANG_CXX_LANGUAGE_STANDARD": "c++17",
            "CLANG_CXX_LIBRARY": "libc++",
            "CLANG_ENABLE_OBJC_ARC": "YES",
            "GCC_ENABLE_CPP_EXCEPTIONS": "YES",
            "MACOSX_DEPLOYMENT_TARGET": "14.4",
            "OTHER_LDFLAGS": [
              "-framework CoreAudio",
              "-framework AudioToolbox",
              "-framework Foundation"
            ]
          }
        } ]
      ]
    }
  ]
}
