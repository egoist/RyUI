// Media and the device's hardware on the web: the modules of
// hardware-sensors.js (network, location, sensors), hardware-audio.js
// (speech, audio, audioRecord, nowPlaying), hardware-camera.js (camera, scan, vision,
// imageEdit) and hardware-more.js (biometric, webAuth, screen, contacts,
// calendar, library..., and the ones a browser can't do, which say why),
// and pay.js (pay: Apple Pay in Safari, Google Pay elsewhere).
//
// camera.View, maps.View and pay.Button are native views, "asuka.camera",
// "asuka.map" (maps.js) and "asuka.payButton" (pay.js): natives has their
// factories, which the host registers beside the app's own.

import { modules as sensors } from "./hardware-sensors.js";
import { modules as audio } from "./hardware-audio.js";
import { modules as camera, cameraView } from "./hardware-camera.js";
import { modules as more } from "./hardware-more.js";
import { mapView } from "./maps.js";
import { modules as pay, payButton } from "./pay.js";

export function modules(h) {
  return { ...sensors(h), ...audio(h), ...camera(h), ...more(h), ...pay(h) };
}

/** natives are the native views (ui.Native kinds) Asuka's packages make, by kind. */
export const natives = { "asuka.camera": cameraView, "asuka.map": mapView, "asuka.payButton": payButton };
