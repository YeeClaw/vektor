// Vektor site — behaviour.
//
// Two things are interactive on this page.
//
// The colour-scheme toggle. Three states, same as the app: "system" (nothing
// stamped on <html>; the CSS follows prefers-color-scheme), or an explicit
// "light" / "dark" stamped as data-theme, which the CSS lets win. The choice
// is remembered per browser in localStorage; nothing is sent anywhere.
//
// The card carousel. The track is a CSS scroll-snap row and works without
// this file. This adds clickable page dots and auto-advance, and shows the
// dots only when the row overflows.

(function () {
  "use strict";

  var STORAGE_KEY = "vektor-site-theme";
  var ORDER = ["system", "light", "dark"];
  var root = document.documentElement;

  function current() {
    var saved = localStorage.getItem(STORAGE_KEY);
    return ORDER.indexOf(saved) === -1 ? "system" : saved;
  }

  function apply(theme) {
    if (theme === "system") {
      root.removeAttribute("data-theme");
      localStorage.removeItem(STORAGE_KEY);
    } else {
      root.setAttribute("data-theme", theme);
      localStorage.setItem(STORAGE_KEY, theme);
    }
    var label = document.querySelector("[data-theme-label]");
    if (label) label.textContent = theme;
  }

  function next(theme) {
    return ORDER[(ORDER.indexOf(theme) + 1) % ORDER.length];
  }

  // Apply the saved choice as early as possible (script is deferred, so this
  // runs before first paint of most content but after the CSS is parsed).
  apply(current());

  var button = document.querySelector("[data-theme-toggle]");
  if (button) {
    button.addEventListener("click", function () {
      apply(next(current()));
    });
  }

  // `t` cycles the theme here too, matching the app's binding. Inert while
  // typing in a field, same rule as the app.
  document.addEventListener("keydown", function (event) {
    if (event.key !== "t" || event.metaKey || event.ctrlKey || event.altKey) return;
    var tag = (event.target && event.target.tagName) || "";
    if (/^(INPUT|TEXTAREA|SELECT)$/.test(tag) || event.target.isContentEditable) return;
    apply(next(current()));
  });
})();

// ---- Card carousel ----------------------------------------------------------
//
// Model: the track scrolls natively; this file only knows about *pages*. A
// page is one screenful of cards. Page positions are computed from the
// layout (and recomputed on resize), clamped to the scroll range, so the last
// page always lands flush at the end and every dot is reachable. The current
// page is whichever page position is nearest the scroll offset.
//
// It auto-advances one page every `data-carousel-interval` ms (default 6000),
// wrapping to the first. It pauses while the pointer is over it, while
// anything inside it has focus, for a while after the user scrolls it by
// hand, and while the tab is hidden. It never auto-advances at all when the
// user prefers reduced motion.
(function () {
  "use strict";

  var REDUCED = window.matchMedia("(prefers-reduced-motion: reduce)");
  var MANUAL_PAUSE = 10000; // ms of quiet after a hand scroll before resuming

  var carousels = document.querySelectorAll("[data-carousel]");
  Array.prototype.forEach.call(carousels, function (root) {
    var track = root.querySelector(".carousel-track");
    var dotsBox = root.querySelector("[data-carousel-dots]");
    if (!track) return;
    var cards = Array.prototype.slice.call(track.children);
    if (cards.length < 2) return;

    var interval = parseInt(root.getAttribute("data-carousel-interval"), 10) || 6000;
    var pages = [];   // scrollLeft for each page
    var dots = [];    // one button per page

    // ----- geometry -----

    // A card's left edge in the track's scroll coordinates, minus the track's
    // own padding, so scrollLeft = cardLeft(i) shows card i at the snap point.
    function cardLeft(i) {
      var pad = parseFloat(getComputedStyle(track).paddingLeft) || 0;
      return cards[i].getBoundingClientRect().left - track.getBoundingClientRect().left
        + track.scrollLeft - pad;
    }

    function maxScroll() {
      return Math.max(0, track.scrollWidth - track.clientWidth);
    }

    // How many whole cards fit in view: the page size.
    function perPage() {
      var w = cards[0].getBoundingClientRect().width;
      var gap = cards.length > 1 ? cardLeft(1) - cardLeft(0) - w : 0;
      return Math.max(1, Math.floor((track.clientWidth + gap) / (w + gap)));
    }

    // Recompute page positions and rebuild the dots. On load and resize.
    function layout() {
      var n = perPage(), max = maxScroll();
      pages = [];
      for (var i = 0; i < cards.length; i += n) {
        var x = Math.min(cardLeft(i), max);
        // Two pages that clamp to the same spot are one page.
        if (!pages.length || x > pages[pages.length - 1] + 1) pages.push(x);
      }
      if (dotsBox) {
        dotsBox.textContent = "";
        dots = pages.map(function (_, i) {
          var b = document.createElement("button");
          b.type = "button";
          b.setAttribute("aria-label", "Page " + (i + 1) + " of " + pages.length);
          b.addEventListener("click", function () { goTo(i); restartTimer(); });
          dotsBox.appendChild(b);
          return b;
        });
      }
    }

    function currentPage() {
      var x = track.scrollLeft, best = 0, bestDist = Infinity;
      pages.forEach(function (p, i) {
        var d = Math.abs(p - x);
        if (d < bestDist) { bestDist = d; best = i; }
      });
      return best;
    }

    // ----- movement -----

    var programmatic = false; // true while a scroll we started is in flight

    function goTo(i) {
      i = Math.max(0, Math.min(pages.length - 1, i));
      programmatic = true;
      // "auto" defers to the CSS scroll-behavior: smooth unless the user asked
      // for reduced motion.
      track.scrollTo({ left: pages[i], behavior: "auto" });
      // Smooth scrolls fire many scroll events; treat them as ours until the
      // track has been still for a moment.
      clearTimeout(settleTimer);
      settleTimer = setTimeout(function () { programmatic = false; }, 600);
    }
    var settleTimer;

    function update() {
      var overflows = track.scrollWidth > track.clientWidth + 1;
      if (dotsBox) dotsBox.hidden = !overflows;
      if (!overflows) { track.removeAttribute("data-edges"); return; }
      // Which edges have content behind them; the CSS fades only those.
      var atStart = track.scrollLeft <= 1;
      var atEnd = track.scrollLeft >= maxScroll() - 1;
      track.setAttribute("data-edges", atStart ? "end" : atEnd ? "start" : "both");
      var i = currentPage();
      dots.forEach(function (d, j) {
        if (j === i) d.setAttribute("aria-current", "true"); else d.removeAttribute("aria-current");
      });
    }

    // ----- autoplay -----

    var timer = null;
    var hovered = false, focused = false, manualUntil = 0;

    function mayPlay() {
      return !REDUCED.matches
        && !document.hidden
        && !hovered && !focused
        && Date.now() >= manualUntil
        && pages.length > 1;
    }

    function tick() {
      if (mayPlay()) goTo((currentPage() + 1) % pages.length);
    }

    function restartTimer() {
      clearInterval(timer);
      timer = setInterval(tick, interval);
    }

    // Pause conditions. Each just flips a flag; tick() consults them, so
    // resuming needs no bookkeeping.
    root.addEventListener("pointerenter", function () { hovered = true; });
    root.addEventListener("pointerleave", function () { hovered = false; });
    root.addEventListener("focusin", function () { focused = true; });
    root.addEventListener("focusout", function () { focused = false; });

    // Left/right arrows while the track itself has focus page like the
    // dots do; the browser would otherwise scroll by pixels.
    track.addEventListener("keydown", function (event) {
      if (event.target !== track) return;
      if (event.key === "ArrowLeft") { event.preventDefault(); goTo(currentPage() - 1); }
      if (event.key === "ArrowRight") { event.preventDefault(); goTo(currentPage() + 1); }
    });

    var pending = false;
    track.addEventListener("scroll", function () {
      // A scroll we did not start is the user's: hold off for a while, and
      // reset the interval so the next auto-advance is a full period away.
      if (!programmatic) { manualUntil = Date.now() + MANUAL_PAUSE; restartTimer(); }
      if (pending) return;
      pending = true;
      requestAnimationFrame(function () { pending = false; update(); });
    }, { passive: true });

    var resizeTimer;
    window.addEventListener("resize", function () {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(function () { layout(); update(); }, 100);
    });

    layout();
    update();
    restartTimer();
  });
})();
