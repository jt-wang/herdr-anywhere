# Writes site/index.html and site/<lang>/index.html from strings.py. Run: python3 site-src/build.py
import json
import re
from html import escape
from pathlib import Path

from strings import INSTALL, NOTFOUND, PAGES, POLICY

SITE = Path(__file__).resolve().parent.parent / "site"


def jpeg_size(path):
    """(width, height) of a JPEG, from its frame header."""
    b = path.read_bytes()
    i = 2
    while i < len(b):
        marker, length = b[i + 1], int.from_bytes(b[i + 2:i + 4], "big")
        if 0xC0 <= marker <= 0xCF and marker not in (0xC4, 0xC8, 0xCC):
            return int.from_bytes(b[i + 7:i + 9], "big"), int.from_bytes(b[i + 5:i + 7], "big")
        i += 2 + length
    raise ValueError(f"{path}: no frame header")


def shot_height(img, v):
    """A phone picture's height at 780 wide: the type picture ends where that language's keyboard starts."""
    light, dark = (jpeg_size(SITE / f"{img}-{v}{t}.jpg") for t in ("light", "dark"))
    assert light == dark and light[0] == 780, (img, v, light, dark)
    return light[1]
ORIGIN = "https://herdr-anywhere.jingtao.io/"
APK = "https://github.com/jt-wang/herdr-anywhere/releases/latest/download/Herdr-Anywhere.apk"
REPO = "https://github.com/jt-wang/herdr-anywhere"
VERSION = json.load(open(Path(__file__).resolve().parent.parent / "app/nsh.json"))["version"]
HREFLANG = {"en": "en", "zh": "zh-Hans", "zh-hant": "zh-Hant", "es": "es", "ja": "ja", "ko": "ko", "ru": "ru"}

# Herdr's ram, the mark the app's own header shows. The drawing's transparent margin is cropped so the tile fills the box.
_ram = open(Path(__file__).resolve().parent.parent / "app/assets/brand/herdr-anywhere.svg").read()
_ram_inner = re.sub(r"<title.*?</title>|<desc.*?</desc>", "", _ram[_ram.index(">") + 1:_ram.rindex("</svg>")], flags=re.S).strip()
BRAND = f'<svg width="24" height="24" viewBox="64 64 896 896" aria-hidden="true" focusable="false">{_ram_inner}</svg>'
FAVICON = f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="64 64 896 896">{_ram_inner}</svg>\n'
GLOBE = '<svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><g fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.5 2.7 3.8 5.7 3.8 9s-1.3 6.3-3.8 9c-2.5-2.7-3.8-5.7-3.8-9S9.5 5.7 12 3Z"/></g></svg>'
SUN = '<svg class="i-sun" width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><g fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="12" r="4"/><path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.3 5.3l1.4 1.4M17.3 17.3l1.4 1.4M5.3 18.7l1.4-1.4M17.3 6.7l1.4-1.4"/></g></svg>'
MOON = '<svg class="i-moon" width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5Z"/></svg>'
X = '<svg width="14" height="14" viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M17.8 3h3.1l-6.8 7.7L22 21h-6.2l-4.9-6.3L5.3 21H2.2l7.2-8.3L1.8 3h6.4l4.4 5.8L17.8 3Zm-1.1 16.2h1.7L7.4 4.7H5.6l11.1 14.5Z"/></svg>'

# Before first paint: the js class, and the theme the visitor picked last time.
HEAD_SCRIPT = '<script>(function(r){r.classList.replace("no-js","js");try{var t=localStorage.getItem("theme");if(t==="light"||t==="dark")r.setAttribute("data-theme",t)}catch(e){}})(document.documentElement);</script>'
# Right after the video tag, so the page's theme picks its video before the light one starts loading.
FILM_SCRIPT = '<script>(function(v,r){var t=r.getAttribute("data-theme")||(matchMedia("(prefers-color-scheme: dark)").matches?"dark":"light");v.poster=v.getAttribute("data-"+t+"-poster");var s=v.getAttribute("data-"+t+"-src");if(v.getAttribute("src")!==s)v.src=s})(document.getElementById("film"),document.documentElement);</script>'


OK = ' class="ok"'


def attr(text):
    return escape(text).replace("&#x27;", "'")


def heart(label):
    return f'<svg width="14" height="14" viewBox="0 0 24 24" role="img" aria-label="{escape(label)}"><path fill="currentColor" d="M12 21s-7.5-4.6-10-9.3C.4 8.4 2.3 4.5 6 4.5c2.1 0 3.4 1.1 4.2 2.3h3.6c.8-1.2 2.1-2.3 4.2-2.3 3.7 0 5.6 3.9 4 7.2C19.5 16.4 12 21 12 21Z"/></svg>'


def card(key):
    """The picture a link to the page shows when it is shared: share-card.png, share-card-<lang>.png."""
    return "share-card.png" if key == "en" else f"share-card-{key}.png"


def page(key):
    t = PAGES[key]
    e = lambda k: attr(t[k])
    home = "/" + t["path"]
    url = ORIGIN + t["path"]
    v = "" if key == "en" else key + "-"
    alternates = "\n".join(
        f'  <link rel="alternate" hreflang="{HREFLANG[k]}" href="{ORIGIN + p["path"]}">' for k, p in PAGES.items()
    ) + f'\n  <link rel="alternate" hreflang="x-default" href="{ORIGIN}">'
    locales = "\n".join(
        f'  <meta property="og:locale:alternate" content="{p["og_locale"]}">' for k, p in PAGES.items() if k != key
    )
    langs = "\n".join(
        f'          <li><a href="/{p["path"]}" hreflang="{HREFLANG[k]}" lang="{p["html_lang"]}"'
        + (' aria-current="page"' if k == key else "") + f'>{p["name"]}</a></li>'
        for k, p in PAGES.items()
    )
    features = "\n".join(
        f'''        <li class="feature reveal">
          <figure class="shot tall"><picture><source srcset="/{img}-{v}dark.jpg" media="(prefers-color-scheme: dark)" data-theme-source="dark"><img src="/{img}-{v}light.jpg" alt="{e(alt)}" width="780" height="{shot_height(img, v)}"></picture></figure>
          <h3>{e(title)}</h3>
        </li>''' for img, alt, title in [("type", "f1_alt", "f1"), ("agents", "f2_alt", "f2"), ("needs", "f3_alt", "f3")]
    )
    facts = "\n".join(
        f'        <div class="fact"><dt>{escape(dt)}</dt><dd{OK if i == 0 else ""}>{escape(dd)}</dd></div>'
        for i, (dt, dd) in enumerate(t["facts"])
    )
    needs, working, done = (escape(w) for w in t["rows"])
    paste = escape(t["paste"].format(url=INSTALL))
    version = f'<a class="version" href="{REPO}/releases/latest">{escape(t["version"].format(version=VERSION))}</a>'
    return f'''<!doctype html>
<html lang="{t["html_lang"]}" class="no-js">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light dark">
  <meta name="theme-color" content="#F6F6F6" media="(prefers-color-scheme: light)">
  <meta name="theme-color" content="#171717" media="(prefers-color-scheme: dark)">
  <title>{e("title")}</title>
  <meta name="description" content="{e("description")}">
  <link rel="canonical" href="{url}">
{alternates}
  <meta property="og:type" content="website">
  <meta property="og:url" content="{url}">
  <meta property="og:locale" content="{t["og_locale"]}">
{locales}
  <meta property="og:title" content="{e("og_title")}">
  <meta property="og:description" content="{e("description")}">
  <meta property="og:image" content="{ORIGIN}{card(key)}">
  <meta property="og:image:width" content="1200">
  <meta property="og:image:height" content="630">
  <meta name="twitter:card" content="summary_large_image">
  <meta name="twitter:site" content="@thejingtao">
  <meta name="twitter:creator" content="@thejingtao">
  <meta name="twitter:image" content="{ORIGIN}{card(key)}">
  <link rel="icon" href="/favicon.svg" type="image/svg+xml">
  <link rel="stylesheet" href="/site.css">
  {HEAD_SCRIPT}
  <script src="/site.js" defer></script>
</head>
<body>
  <a class="skip-link" href="#main">{e("skip")}</a>
  <header class="site-header wrap">
    <a class="brand" href="{home}" aria-label="Herdr Anywhere">{BRAND}<span>Herdr Anywhere</span></a>
    <div class="tools">
      <details class="lang">
        <summary aria-label="{e("language")}: {e("name")}">{GLOBE}<span class="lang-name">{e("name")}</span></summary>
        <ul>
{langs}
        </ul>
      </details>
      <button type="button" class="theme js-only" data-label-dark="{e("theme_dark")}" data-label-light="{e("theme_light")}" aria-label="{e("theme_dark")}">{SUN}{MOON}</button>
      <a class="btn btn-primary btn-small" href="#get">{e("get_it")}</a>
    </div>
  </header>

  <main id="main">
    <section class="hero wrap" aria-labelledby="hero-title">
      <div class="hero-copy">
        <h1 id="hero-title"><span class="h1-line">{escape(t["h1"][0])}</span><span class="h1-line">{escape(t["h1"][1])}</span></h1>
        <p class="lede">{e("lede")}</p>
        <div class="cta">
          <a class="btn btn-primary" href="#get">{e("cta1")}</a>
          <a class="btn btn-secondary" href="{APK}">{e("cta2")}</a>
        </div>
        <p class="version-line">{version}</p>
      </div>
      <figure class="film">
        <video id="film" src="/herdr-anywhere-{v}light.mp4" poster="/poster-{v}light.jpg" data-light-src="/herdr-anywhere-{v}light.mp4" data-dark-src="/herdr-anywhere-{v}dark.mp4" data-light-poster="/poster-{v}light.jpg" data-dark-poster="/poster-{v}dark.jpg" autoplay muted loop playsinline preload="metadata" aria-label="{e("film")}"></video>
        {FILM_SCRIPT}
      </figure>
    </section>

    <section class="section wrap" aria-labelledby="use-title">
      <h2 id="use-title" class="reveal">{e("use_title")}</h2>
      <ul class="features">
{features}
      </ul>
      <ul class="stats">
        <li class="stat reveal"><b>{escape(t["s1"][0])}</b><span>{escape(t["s1"][1])}</span></li>
        <li class="stat reveal"><b>{escape(t["s2"][0])}</b><span>{escape(t["s2"][1])}</span></li>
      </ul>
    </section>

    <section class="section wrap" aria-labelledby="private-title">
      <h2 id="private-title" class="reveal">{e("private_title")}</h2>
      <dl class="facts reveal">
{facts}
      </dl>
    </section>

    <section class="section wrap" aria-labelledby="setup-title">
      <h2 id="setup-title" class="reveal">{e("setup_title")}</h2>
      <ol class="steps">
        <li class="step reveal">
          <figure class="shot art" aria-hidden="true"><div class="chat"><p class="you">{e("chat_you")}</p><p class="done">{e("chat_done")}</p></div></figure>
          <span class="step-num">1</span><h3>{e("st1")}</h3>
        </li>
        <li class="step reveal">
          <figure class="shot"><picture><source srcset="/scan-{v}dark.jpg" media="(prefers-color-scheme: dark)" data-theme-source="dark"><img src="/scan-{v}light.jpg" alt="{e("scan_alt")}" width="1280" height="720"></picture></figure>
          <span class="step-num">2</span><h3>{t["st2"]}</h3>
        </li>
        <li class="step reveal">
          <figure class="shot art" aria-hidden="true"><ul class="rows"><li><b>billing · Kimi</b><span class="ask">{needs}</span></li><li><b>web-app · Pi</b><span>{working}</span></li><li><b>acme-api · Claude Code</b><span>{done}</span></li></ul></figure>
          <span class="step-num">3</span><h3>{e("st3")}</h3>
        </li>
      </ol>
    </section>

    <section class="section wrap" id="get" aria-labelledby="get-title">
      <div class="get reveal">
        <h2 id="get-title">{e("get_title")}</h2>
        <div class="ways">
          <div class="way">
            <h3>{e("way1")}</h3>
            <ol class="get-steps">
              <li><span class="step-num">1</span><div><p>{e("g1")}</p>
                <div class="command-row">
                  <pre class="command-box"><code id="setup-line">{paste}</code></pre>
                  <button type="button" class="btn btn-secondary js-only" data-copy-target="setup-line" data-copy-status="copy-status" data-copied="{e("copied")}" data-copy-fail="{e("copy_fail")}">{e("copy")}</button>
                </div>
                <p id="copy-status" class="copy-status" role="status"></p></div></li>
              <li><span class="step-num">2</span><div><p>{e("g2")}</p><div class="links"><a class="btn btn-primary" href="https://testflight.apple.com/join/ddBQGV2T">{e("iphone")}</a><a class="btn btn-primary" href="{APK}">{e("android")}</a>{version}</div></div></li>
              <li><span class="step-num">3</span><div><p>{t["g3"]}</p></div></li>
            </ol>
            <p class="ios-note">{e("ios")}</p>
            <div class="links">
              <a class="btn btn-secondary" href="https://x.com/thejingtao">{X}{e("follow")}</a>
            </div>
          </div>
          <div class="way">
            <h3>{e("way2")}</h3>
            <p>{e("build")}</p>
            <div class="links">
              <a class="btn btn-secondary" href="{REPO}#build-from-source">{e("guide")}</a>
              <a class="text-link" href="{REPO}">{e("source")}</a>
            </div>
          </div>
        </div>
      </div>
    </section>
  </main>

  <footer class="site-footer wrap">
    <a class="footer-x" href="https://x.com/thejingtao">{t["made"].format(heart=heart(t["love"]))}</a>
    <p class="footer-note">{t["footer"]}</p>
    <a class="footer-link" href="/{t["path"]}privacy/">{e("privacy")}</a>
  </footer>
</body>
</html>
'''


def small_page(key, sub, title, description, body, status_page=False):
    """The privacy policy or the not-found page in one language: the home page's header and footer around `body`."""
    t = PAGES[key]
    e = lambda k: attr(t[k])
    home = "/" + t["path"]
    url = ORIGIN + t["path"] + sub
    lsub = "" if status_page else sub
    langs = "\n".join(
        f'          <li><a href="/{p["path"]}{lsub}" hreflang="{HREFLANG[k]}" lang="{p["html_lang"]}"'
        + (' aria-current="page"' if k == key else "") + f'>{p["name"]}</a></li>'
        for k, p in PAGES.items()
    )
    links = "" if status_page else "\n".join(
        f'  <link rel="alternate" hreflang="{HREFLANG[k]}" href="{ORIGIN + p["path"] + sub}">' for k, p in PAGES.items()
    ) + f'\n  <link rel="canonical" href="{url}">\n  <meta property="og:url" content="{url}">'
    robots = '\n  <meta name="robots" content="noindex">' if status_page else ""
    return f'''<!doctype html>
<html lang="{t["html_lang"]}" class="no-js">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light dark">
  <meta name="theme-color" content="#F6F6F6" media="(prefers-color-scheme: light)">
  <meta name="theme-color" content="#171717" media="(prefers-color-scheme: dark)">
  <title>{attr(title)}</title>
  <meta name="description" content="{attr(description)}">{robots}
{links}
  <meta property="og:type" content="website">
  <meta property="og:locale" content="{t["og_locale"]}">
  <meta property="og:title" content="{attr(title)}">
  <meta property="og:description" content="{attr(description)}">
  <meta property="og:image" content="{ORIGIN}{card(key)}">
  <meta name="twitter:card" content="summary_large_image">
  <meta name="twitter:site" content="@thejingtao">
  <link rel="icon" href="/favicon.svg" type="image/svg+xml">
  <link rel="stylesheet" href="/site.css">
  {HEAD_SCRIPT}
  <script src="/site.js" defer></script>
</head>
<body>
  <a class="skip-link" href="#main">{e("skip")}</a>
  <header class="site-header wrap">
    <a class="brand" href="{home}" aria-label="Herdr Anywhere">{BRAND}<span>Herdr Anywhere</span></a>
    <div class="tools">
      <details class="lang">
        <summary aria-label="{e("language")}: {e("name")}">{GLOBE}<span class="lang-name">{e("name")}</span></summary>
        <ul>
{langs}
        </ul>
      </details>
      <button type="button" class="theme js-only" data-label-dark="{e("theme_dark")}" data-label-light="{e("theme_light")}" aria-label="{e("theme_dark")}">{SUN}{MOON}</button>
      <a class="btn btn-primary btn-small" href="{home}#get">{e("get_it")}</a>
    </div>
  </header>

  <main id="main">
{body}
  </main>

  <footer class="site-footer wrap">
    <a class="footer-x" href="https://x.com/thejingtao">{t["made"].format(heart=heart(t["love"]))}</a>
    <p class="footer-note">{t["footer"]}</p>
  </footer>
</body>
</html>
'''


# The policy the app links to (/privacy/, the English one) and App Store Connect names. Each sentence is what the app,
# the companion and Tailcat do; change it with them, in every language (strings.py POLICY).
def privacy_page(key):
    p = POLICY[key]
    items = "\n".join(f"        <li>{escape(i, quote=False)}</li>" for i in p["items"])
    body = f'''    <article class="policy wrap" aria-labelledby="policy-title">
      <h1 id="policy-title">{escape(p["h1"])}</h1>
      <p class="policy-lede">{escape(p["lede"], quote=False)}</p>
      <ul>
{items}
      </ul>
      <p>{p["questions"].format(issues=f'href="{REPO}/issues"')}</p>
    </article>'''
    return small_page(key, "privacy/", p["title"], p["lede"], body)


# What an address that isn't on the site shows, in the language of the folder it was asked for under.
def notfound_page(key):
    n = NOTFOUND[key]
    body = f'''    <article class="policy wrap" aria-labelledby="nf-title">
      <h1 id="nf-title">{escape(n["h1"])}</h1>
      <p class="policy-lede">{escape(n["body"], quote=False)}</p>
      <p><a class="btn btn-primary" href="/{PAGES[key]["path"]}">{escape(n["home"])}</a></p>
    </article>'''
    return small_page(key, "404.html", n["title"], n["body"], body, status_page=True)


if __name__ == "__main__":
    (SITE / "favicon.svg").write_text(FAVICON)
    for key, t in PAGES.items():
        base = SITE / t["path"]
        pages = [(base / "index.html", page)] + [(base / "privacy" / "index.html", privacy_page)] * (key in POLICY) + [(base / "404.html", notfound_page)] * (key in NOTFOUND)
        for out, make in pages:
            html = make(key)
            out.parent.mkdir(parents=True, exist_ok=True)
            out.write_text(html)
            print(out.relative_to(SITE.parent))
