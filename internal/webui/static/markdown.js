// A compact, dependency-free, offline Markdown renderer.
//
// Security model: the raw source is HTML-escaped FIRST, then a small set of
// markdown constructs are turned into a fixed allow-list of safe tags. Because
// escaping happens before any tag is introduced, model output cannot inject
// HTML. Link targets are additionally restricted to safe schemes.
//
// This intentionally covers the constructs LLMs emit in practice (headings,
// lists, code fences, inline code, bold/italic, links, blockquotes, rules)
// rather than the full CommonMark spec. Swap in a vendored library here if you
// need exhaustive coverage.
(function () {
  "use strict";

  function esc(s) {
    return s
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  function escAttr(s) {
    return esc(s).replace(/"/g, "&quot;");
  }

  function safeUrl(u) {
    const t = u.trim();
    // Allow http(s), mailto, anchors, and relative/absolute paths only.
    if (/^(https?:\/\/|mailto:|#|\/|\.)/i.test(t)) return t;
    return "#";
  }

  // inline operates on already-escaped text and emits safe inline tags.
  function inline(text) {
    const codes = [];
    // Protect inline code spans from further processing.
    text = text.replace(/`([^`]+)`/g, function (_, p1) {
      codes.push("<code>" + p1 + "</code>");
      return "\u0000" + (codes.length - 1) + "\u0000";
    });

    text = text.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, function (_, label, url) {
      return '<a href="' + escAttr(safeUrl(url)) + '" target="_blank" rel="noopener noreferrer">' + label + "</a>";
    });

    text = text.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
    text = text.replace(/__([^_]+)__/g, "<strong>$1</strong>");
    text = text.replace(/(^|[^*])\*([^*\n]+)\*/g, "$1<em>$2</em>");
    text = text.replace(/(^|[^_\w])_([^_\n]+)_/g, "$1<em>$2</em>");
    text = text.replace(/~~([^~]+)~~/g, "<del>$1</del>");

    text = text.replace(/\u0000(\d+)\u0000/g, function (_, idx) {
      return codes[+idx];
    });
    return text;
  }

  function renderMarkdown(src) {
    if (!src) return "";
    const lines = String(src).replace(/\r\n?/g, "\n").split("\n");
    const out = [];
    let para = [];
    let i = 0;

    function flushPara() {
      if (para.length) {
        out.push("<p>" + inline(esc(para.join("\n")).replace(/\n/g, "<br>")) + "</p>");
        para = [];
      }
    }

    while (i < lines.length) {
      const line = lines[i];

      const fence = line.match(/^\s*```(.*)$/);
      if (fence) {
        flushPara();
        const lang = fence[1].trim();
        const buf = [];
        i++;
        while (i < lines.length && !/^\s*```/.test(lines[i])) {
          buf.push(lines[i]);
          i++;
        }
        i++; // consume closing fence (no-op past EOF while streaming)
        const cls = lang ? ' class="language-' + escAttr(lang) + '"' : "";
        out.push("<pre><code" + cls + ">" + esc(buf.join("\n")) + "</code></pre>");
        continue;
      }

      if (/^\s*$/.test(line)) {
        flushPara();
        i++;
        continue;
      }

      const h = line.match(/^(#{1,6})\s+(.*)$/);
      if (h) {
        flushPara();
        const lvl = h[1].length;
        out.push("<h" + lvl + ">" + inline(esc(h[2])) + "</h" + lvl + ">");
        i++;
        continue;
      }

      if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(line)) {
        flushPara();
        out.push("<hr>");
        i++;
        continue;
      }

      if (/^\s*>\s?/.test(line)) {
        flushPara();
        const buf = [];
        while (i < lines.length && /^\s*>\s?/.test(lines[i])) {
          buf.push(lines[i].replace(/^\s*>\s?/, ""));
          i++;
        }
        out.push("<blockquote>" + inline(esc(buf.join("\n")).replace(/\n/g, "<br>")) + "</blockquote>");
        continue;
      }

      const ordered = /^(\s*)\d+[.)]\s+/.test(line);
      const bulleted = /^(\s*)[-*+]\s+/.test(line);
      if (ordered || bulleted) {
        flushPara();
        const tag = ordered ? "ol" : "ul";
        const re = ordered ? /^(\s*)\d+[.)]\s+(.*)$/ : /^(\s*)[-*+]\s+(.*)$/;
        const items = [];
        while (i < lines.length) {
          const m = lines[i].match(re);
          if (!m) break;
          items.push("<li>" + inline(esc(m[2])) + "</li>");
          i++;
        }
        out.push("<" + tag + ">" + items.join("") + "</" + tag + ">");
        continue;
      }

      para.push(line);
      i++;
    }

    flushPara();
    return out.join("\n");
  }

  window.renderMarkdown = renderMarkdown;
})();
