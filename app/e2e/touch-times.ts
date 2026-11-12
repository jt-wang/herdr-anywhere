// A script e2e/ios-tui-scroll.sh puts into the terminal page of the app it builds for the test (from a copy of app/,
// so the app itself is unchanged), to see when the page gets each touch. For each gesture it sends the program, as
// typed input that e2e/tui-panels.py writes down as a "touch" line: when the touch came down, the first move and the
// lift, each as the event's own time and the time the page got to it (ms from the touch's own time), and when the
// page's hold timer (450 ms after the touch; a hold selects words instead of scrolling) ran. It only listens: the
// page's own listeners and its timer run as they do in the app.
export const TOUCH_TIMES = `(function () {
  var g = null, at = setTimeout;
  window.setTimeout = function (f, ms) {
    var mine = g;
    if (mine && ms === 450 && typeof f === 'function') {
      var run = f;
      arguments[0] = function () { if (mine.hold == null) mine.hold = performance.now(); return run.apply(this, arguments); };
    }
    return at.apply(window, arguments);
  };
  var both = function (e) { return [Math.round(e.timeStamp - g.t0), Math.round(performance.now() - g.t0)].join('/'); };
  window.addEventListener('touchstart', function (e) {
    if (e.touches.length !== 1) return;
    g = { t0: e.timeStamp, moves: 0 }; g.start = both(e);
  }, { capture: true, passive: true });
  window.addEventListener('touchmove', function (e) {
    if (!g) return;
    g.moves++;
    if (!g.move) g.move = both(e);
  }, { capture: true, passive: true });
  window.addEventListener('touchend', function (e) {
    if (!g || e.touches.length) return;
    var d = g; d.end = both(e); g = null;
    at(function () {
      var hold = d.hold == null ? '-' : String(Math.round(d.hold - d.t0));
      var line = 'start=' + d.start + ' move=' + (d.move || '-') + ' hold=' + hold + ' end=' + d.end + ' moves=' + d.moves;
      if (window.nsh) window.nsh.postMessage(JSON.stringify({ t: 'input', data: '\\x1b_hm-touch ' + line + '\\x1b\\\\' }));
    }, 0);
  }, { capture: true, passive: true });
})();`
