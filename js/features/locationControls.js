// Sets up geolocation tracking and compass-based bearing updates.
export function initLocationControlsFeature({ map, maplibregl, userLocation }) {
    const compass = {
        state: "OFF",
        heading: null,
        _handler: null,
        _raf: null,
        _permitted: false,

        isAvailable() {
            return "DeviceOrientationEvent" in window || "ondeviceorientationabsolute" in window;
        },

        async requestPermission() {
            if (this._permitted) return;
            if (typeof DeviceOrientationEvent !== "undefined" && typeof DeviceOrientationEvent.requestPermission === "function") {
                try {
                    const result = await DeviceOrientationEvent.requestPermission();
                    this._permitted = result === "granted";
                } catch (_) {
                    // ignore
                }
            } else {
                this._permitted = true;
            }
        },

        _eventToHeading(event) {
            if (event.webkitCompassHeading != null) return event.webkitCompassHeading;
            if (event.alpha != null) return (360 - event.alpha) % 360;
            return null;
        },

        start(targetMap) {
            if (this._handler) return;

            let pending = null;
            const self = this;

            this._handler = function(event) {
                const heading = self._eventToHeading(event);
                if (heading === null) return;
                pending = heading;

                if (!self._raf) {
                    self._raf = requestAnimationFrame(function() {
                        self._raf = null;
                        if (self.state !== "COMPASS" || pending === null) return;
                        self.heading = pending;
                        targetMap.transform.setBearing(pending);
                        targetMap.fire("rotate");
                        targetMap.triggerRepaint();
                    });
                }
            };

            if ("ondeviceorientationabsolute" in window) {
                window.addEventListener("deviceorientationabsolute", this._handler);
            } else {
                window.addEventListener("deviceorientation", this._handler);
            }
            this.state = "COMPASS";
        },

        stop() {
            if (this._handler) {
                window.removeEventListener("deviceorientationabsolute", this._handler);
                window.removeEventListener("deviceorientation", this._handler);
                this._handler = null;
            }
            if (this._raf) {
                cancelAnimationFrame(this._raf);
                this._raf = null;
            }
            this.heading = null;
            this.state = "OFF";
        },
    };

    function setupCompassTracking(geolocateControl) {
        if (!compass.isAvailable()) return;

        geolocateControl._updateCamera = function(position) {
            this._map.transform.setCenter(new maplibregl.LngLat(position.coords.longitude, position.coords.latitude));
            this._map.triggerRepaint();

            if (compass.state === "OFF" && compass._permitted) {
                compass.start(this._map);
            }
        };

        geolocateControl.on("trackuserlocationstart", () => {
            compass.requestPermission();
        });

        map.on("dragstart", (e) => {
            if (e.originalEvent && compass.state === "COMPASS") {
                compass.stop();
            }
        });

        geolocateControl.on("trackuserlocationend", () => {
            if (compass.state === "COMPASS") {
                compass.stop();
            }
        });
    }

    const geolocateControl = new maplibregl.GeolocateControl({
        positionOptions: { enableHighAccuracy: true },
        trackUserLocation: true,
        showUserLocation: true,
        showAccuracyCircle: true,
        showUserHeading: true,
    });

    map.addControl(geolocateControl, "top-right");
    setupCompassTracking(geolocateControl);

    if (userLocation?.accuracy) {
        map.once("load", () => {
            geolocateControl.trigger();
        });
    }

    function stopSearchFlyTrackingConflicts() {
        if (compass.state === "COMPASS") {
            compass.stop();
        }

        if (geolocateControl && geolocateControl._watchState === "ACTIVE_LOCK") {
            if (typeof geolocateControl._onControlClick === "function") {
                geolocateControl._onControlClick();
            } else if (geolocateControl._geolocateButton) {
                geolocateControl._geolocateButton.click();
            }
        }
    }

    return {
        stopSearchFlyTrackingConflicts,
    };
}

