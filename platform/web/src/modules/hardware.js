// Media and the device's hardware on the web: the modules of
// hardware-sensors.js (network, location, sensors), hardware-audio.js
// (speech, audio, audioRecord), hardware-camera.js (camera, scan, vision,
// imageEdit) and hardware-more.js (biometric, webAuth, screen, contacts,
// calendar, library..., and the ones a browser can't do, which say why).
//
// camera.View is a native view, "ryui.camera": natives has its factory,
// which the host registers beside the app's own.

import { modules as sensors } from "./hardware-sensors.js";
import { modules as audio } from "./hardware-audio.js";
import { modules as camera, cameraView } from "./hardware-camera.js";
import { modules as more } from "./hardware-more.js";

export function modules(h) {
  return { ...sensors(h), ...audio(h), ...camera(h), ...more(h) };
}

/** natives are the native views (ui.Native kinds) RyUI's packages make, by kind. */
export const natives = { "ryui.camera": cameraView };
