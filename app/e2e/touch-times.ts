// A script the iOS tests put into the terminal page of the app they build for the test (e2e/ios-touch-app.sh, from a
// copy of app/, so the app itself is unchanged), to see when the page gets each touch and what it makes of it. For each
// gesture: when the touch came down, the first move and the lift, each as the event's own time and the time the page
// got to it (ms from the touch's own time); when the page's hold timer ran (450 ms after the touch; a hold selects words
// instead of scrolling or opening); whether the touch stopped a glide (stopped=1: the page then opens nothing); what the
// page told the app in the second after the lift (image:PATH: it asked the app about a picture, which opens it; tap: a
// plain tap; selected: a hold; wheel input is left out); and the app's answer about that picture as the page got it
// (answer=true|false, ms after the touch; false is also what an error gives). It only listens: the page's own
// listeners, timer and messages run as they do in the app.
// It tells the test one line per gesture, "start=… move=… hold=… end=… moves=N stopped=0|1 posts=a,b answer=…", by
//   input: typed input to the terminal's program, as \x1b_hm-touch LINE\x1b\\ (e2e/tui-panels.py writes it down), or
//   label: the label of an invisible element in the page (the last 5 gestures, "hm-touch LINE | LINE"), which an
//          XCUITest reads; for a terminal running a shell, where typed input would land on its command line.
export function touchTimes(via: 'input' | 'label'): string {
  return `(function () {
  var via = '${via}', g = null, last = null, at = setTimeout, said = [], el = null;
  window.setTimeout = function (f, ms) {
    var mine = g;
    if (mine && ms === 450 && typeof f === 'function') {
      var run = f;
      arguments[0] = function () { if (mine.hold == null) mine.hold = performance.now(); return run.apply(this, arguments); };
    }
    return at.apply(window, arguments);
  };
  var raf = window.cancelAnimationFrame;
  window.cancelAnimationFrame = function () { if (g && g.starting) g.stopped = 1; return raf.apply(window, arguments); };
  var wrapped = false;
  function wrap() {
    if (wrapped || !window.nsh || typeof window.nsh.postMessage !== 'function') return;
    wrapped = true;
    var send = window.nsh.postMessage;
    window.nsh.postMessage = function (m) {
      var d = last;
      if (d && !d.done) {
        try { var p = JSON.parse(m); if (p.t !== 'touched' && p.t !== 'input') { d.posts.push(p.t === 'image' ? 'image:' + p.path : p.t); if (p.t === 'image') d.ask = p.id; } } catch (e) {}
      }
      return send.apply(this, arguments);
    };
  }
  function tell(line) {
    if (via === 'input') {
      if (window.nsh) window.nsh.postMessage(JSON.stringify({ t: 'input', data: '\\x1b_hm-touch ' + line + '\\x1b\\\\' }));
      return;
    }
    said.push(line); if (said.length > 5) said.shift();
    show();
  }
  function show() {
    if (!el) {
      el = document.createElement('div');
      el.setAttribute('role', 'status');
      el.style.cssText = 'position:fixed;left:0;top:0;width:1px;height:1px;opacity:0;pointer-events:none;overflow:hidden';
      el.textContent = '.';
      document.body.appendChild(el);
    }
    el.setAttribute('aria-label', 'hm-touch ' + said.join(' | '));
  }
  function line(d) {
    var hold = d.hold == null ? '-' : String(Math.round(d.hold - d.t0));
    return 'start=' + d.start + ' move=' + (d.move || '-') + ' hold=' + hold + ' end=' + d.end + ' moves=' + d.moves +
      ' stopped=' + (d.stopped ? 1 : 0) + ' posts=' + (d.posts.join(',') || '-') + (d.answer ? ' answer=' + d.answer : '');
  }
  // The app's answer about a picture the page asked about: the gesture's line again, with it (label only).
  window.addEventListener('message', function (e) {
    var m; try { m = JSON.parse(e.data); } catch (_) { return; }
    var d = last;
    if (!d || !m || m.t !== 'image' || m.id !== d.ask) return;
    d.answer = (m.picture === true) + '@' + Math.round(performance.now() - d.t0);
    if (d.done && via === 'label' && said.length) { said[said.length - 1] = line(d); show(); }
  }, true);
  var both = function (e, d) { return [Math.round(e.timeStamp - d.t0), Math.round(performance.now() - d.t0)].join('/'); };
  window.addEventListener('touchstart', function (e) {
    wrap();
    if (e.touches.length !== 1) return;
    g = { t0: e.timeStamp, moves: 0, posts: [], starting: true }; g.start = both(e, g);
  }, { capture: true, passive: true });
  // After the page's own touchstart (which stops a glide), not before.
  window.addEventListener('touchstart', function () { if (g) g.starting = false; }, { passive: true });
  window.addEventListener('touchmove', function (e) {
    if (!g) return;
    g.moves++;
    if (!g.move) g.move = both(e, g);
  }, { capture: true, passive: true });
  window.addEventListener('touchend', function (e) {
    if (!g || e.touches.length) return;
    var d = g; d.end = both(e, d); g = null; last = d;
    at(function () {
      d.done = true;
      tell(line(d));
    }, 1000);
  }, { capture: true, passive: true });
})();`
}
