// Loads the compiled native add-on (built into build/Release/syscapture.node by
// node-gyp / electron-rebuild). Required from the Electron MAIN process only — the
// renderer runs with nodeIntegration disabled and cannot load native modules.
module.exports = require("./build/Release/syscapture.node");
