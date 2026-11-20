(function () {
  var reduce = window.matchMedia("(prefers-reduced-motion: reduce)");
  var root = document.documentElement;
  var systemDark = window.matchMedia("(prefers-color-scheme: dark)");

  // The headline's words rise in, one sentence per line.
  var h1 = document.querySelector(".hero h1");
  if (h1 && !reduce.matches) {
    var n = 0;
    h1.querySelectorAll(".h1-line").forEach(function (line) {
      line.innerHTML = line.textContent.trim().split(" ").map(function (w) {
        return '<span class="hw" style="animation-delay:' + (n++ * 80) + 'ms">' + w.replace(/&/g, "&amp;").replace(/</g, "&lt;") + "</span>";
      }).join(" ");
    });
  }

  // Sections ease in once as they scroll into view.
  if ("IntersectionObserver" in window && !reduce.matches) {
    var io = new IntersectionObserver(function (es) {
      es.forEach(function (e) { if (e.isIntersecting) { e.target.classList.remove("pending"); io.unobserve(e.target); } });
    }, { rootMargin: "0px 0px -8% 0px" });
    document.querySelectorAll(".reveal").forEach(function (el) {
      if (el.getBoundingClientRect().top > window.innerHeight) { el.classList.add("pending"); io.observe(el); }
    });
  }

  // Theme: the system's until the header toggle picks one, which is remembered.
  function theme() { return root.getAttribute("data-theme") || (systemDark.matches ? "dark" : "light"); }
  function applyTheme() {
    var t = theme();
    var v = document.getElementById("film");
    if (v) {
      v.poster = v.getAttribute("data-" + t + "-poster");
      var src = v.getAttribute("data-" + t + "-src");
      if (v.getAttribute("src") !== src) {
        var at = v.currentTime, playing = !v.paused;
        v.src = src;
        v.addEventListener("loadedmetadata", function () {
          try { v.currentTime = at; } catch (e) {}
          if (playing) { var p = v.play(); if (p && p.catch) p.catch(function () {}); }
        }, { once: true });
      }
    }
    document.querySelectorAll("[data-theme-source]").forEach(function (s) {
      s.media = s.getAttribute("data-theme-source") === t ? "all" : "not all";
    });
    document.querySelectorAll('meta[name="theme-color"]').forEach(function (m) { m.content = t === "dark" ? "#171717" : "#F6F6F6"; });
    var btn = document.querySelector(".theme");
    if (btn) btn.setAttribute("aria-label", btn.getAttribute(t === "dark" ? "data-label-light" : "data-label-dark"));
  }
  var toggle = document.querySelector(".theme");
  if (toggle) toggle.addEventListener("click", function () {
    var t = theme() === "dark" ? "light" : "dark";
    root.setAttribute("data-theme", t);
    try { localStorage.setItem("theme", t); } catch (e) {}
    applyTheme();
  });
  if (systemDark.addEventListener) systemDark.addEventListener("change", function () { if (!root.getAttribute("data-theme")) applyTheme(); });
  applyTheme();

  // The language list closes on a click outside it or on Escape.
  var lang = document.querySelector(".lang");
  if (lang) {
    document.addEventListener("click", function (e) { if (lang.open && !lang.contains(e.target)) lang.open = false; });
    document.addEventListener("keydown", function (e) { if (e.key === "Escape" && lang.open) { lang.open = false; lang.querySelector("summary").focus(); } });
  }

  // Copy buttons.
  document.querySelectorAll("[data-copy-target]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var text = document.getElementById(btn.dataset.copyTarget).textContent.trim();
      var status = document.getElementById(btn.dataset.copyStatus);
      function done(ok) { if (status) status.textContent = ok ? btn.dataset.copied : btn.dataset.copyFail; }
      if (navigator.clipboard && window.isSecureContext) navigator.clipboard.writeText(text).then(function () { done(true); }, function () { done(false); });
      else done(false);
    });
  });
})();
