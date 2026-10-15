/**
 * Summary HTML utilities: split the append-only signed summary into segments and
 * sanitize the content.
 *
 * The summary is plain concatenation: each segment opens with a signed marker
 * `<div class="summary-sign" data-sig data-author-type data-author>Name · time</div>`
 * then its body. Splitting on the marker is unambiguous (no separate separator).
 *
 * Security: bodies are user HTML rendered via `v-html`, so they run through a whitelist
 * sanitizer — keep allowed tags, drop all attributes except a validated `a[href]`,
 * unwrap unknown tags (text survives, markup does not).
 */

/** Author type for a kid-authored segment, aligned with the backend domain.UploadedByKid. */
export const SUMMARY_AUTHOR_KID = 2;

/** Tags allowed in a segment body. Anything else is unwrapped. */
const ALLOWED_TAGS = new Set([
  "p",
  "br",
  "strong",
  "b",
  "em",
  "i",
  "u",
  "s",
  "code",
  "pre",
  "blockquote",
  "ul",
  "ol",
  "li",
  "h1",
  "h2",
  "h3",
  "h4",
  "a",
]);

/** Safe link schemes; everything else (javascript:, data:, ...) is dropped. */
const SAFE_HREF = /^(https?:|mailto:)/i;

/**
 * Tags whose whole subtree is dropped (not unwrapped): script/style would leak source
 * text, and interactive/embedding tags have no place in a note body.
 */
const DROP_TAGS = new Set([
  "script",
  "style",
  "iframe",
  "object",
  "embed",
  "noscript",
  "template",
  "svg",
  "math",
  "form",
  "input",
  "textarea",
  "select",
  "button",
]);

const ELEMENT_NODE = 1;
const TEXT_NODE = 3;

export interface SummarySegment {
  /** Segment id; empty for the pre-marker text prefix (older summaries; readable, not deletable). */
  sig: string;
  /** Author type (1 parent / 2 kid); 0 when unknown. */
  authorType: number;
  /** Author id; 0 when unknown. */
  authorId: number;
  /** Marker text ("Name · time"); empty when the segment has no marker. */
  signText: string;
  /** Sanitized segment body HTML. */
  bodyHtml: string;
  /** Whether the segment has a signed marker (thus an addressable sig). */
  hasSign: boolean;
}

/** isSignDiv reports whether node is a signed segment marker. */
function isSignDiv(node: Node): boolean {
  if (node.nodeType !== ELEMENT_NODE) return false;
  const el = node as Element;
  return (
    el.tagName.toLowerCase() === "div" &&
    el.classList.contains("summary-sign") &&
    el.hasAttribute("data-sig")
  );
}

/** isSafeHref reports whether an href uses an allowed scheme. */
function isSafeHref(href: string): boolean {
  return SAFE_HREF.test(href.trim());
}

/**
 * sanitizeNode copies node keeping only allowed tags/attributes. Disallowed elements are
 * unwrapped (sanitized children returned; text survives, markup does not). Comments and
 * other non-text/non-element nodes are dropped.
 */
function sanitizeNode(node: Node, doc: Document): Node | null {
  if (node.nodeType === TEXT_NODE) {
    return doc.createTextNode(node.textContent ?? "");
  }
  if (node.nodeType !== ELEMENT_NODE) {
    return null;
  }
  const el = node as Element;
  const tag = el.tagName.toLowerCase();
  if (DROP_TAGS.has(tag)) {
    return null;
  }
  if (!ALLOWED_TAGS.has(tag)) {
    const frag = doc.createDocumentFragment();
    for (const child of Array.from(el.childNodes)) {
      const clean = sanitizeNode(child, doc);
      if (clean) frag.appendChild(clean);
    }
    return frag;
  }
  const out = doc.createElement(tag);
  if (tag === "a") {
    const href = el.getAttribute("href") ?? "";
    if (isSafeHref(href)) {
      out.setAttribute("href", href.trim());
      out.setAttribute("target", "_blank");
      out.setAttribute("rel", "noopener noreferrer");
    }
  }
  for (const child of Array.from(el.childNodes)) {
    const clean = sanitizeNode(child, doc);
    if (clean) out.appendChild(clean);
  }
  return out;
}

/** serialize renders a sanitized node (element/text/fragment) back to HTML. */
function serialize(node: Node, doc: Document): string {
  const container = doc.createElement("div");
  container.appendChild(node);
  return container.innerHTML;
}

/**
 * parseSummary sanitizes the stored summary and splits it into ordered segments.
 * Text stored before the first marker (older summaries) becomes a marker-less prefix segment.
 */
export function parseSummary(html: string): SummarySegment[] {
  if (!html || html.trim() === "") return [];
  const doc = new DOMParser().parseFromString(`<div id="__root">${html}</div>`, "text/html");
  const root = doc.getElementById("__root") ?? doc.querySelector("#__root");
  if (!root) return [];

  const segments: SummarySegment[] = [];
  let current: SummarySegment | null = null;
  const ensure = (): SummarySegment => {
    if (current === null) {
      current = { sig: "", authorType: 0, authorId: 0, signText: "", bodyHtml: "", hasSign: false };
      segments.push(current);
    }
    return current;
  };

  for (const child of Array.from(root.childNodes)) {
    if (isSignDiv(child)) {
      const el = child as Element;
      current = {
        sig: el.getAttribute("data-sig") ?? "",
        authorType: Number(el.getAttribute("data-author-type") ?? "0") || 0,
        authorId: Number(el.getAttribute("data-author") ?? "0") || 0,
        signText: (el.textContent ?? "").trim(),
        bodyHtml: "",
        hasSign: true,
      };
      segments.push(current);
      continue;
    }
    // Drop whitespace-only text between markers so blank segments don't appear.
    if (child.nodeType === TEXT_NODE && (child.textContent ?? "").trim() === "") {
      continue;
    }
    const clean = sanitizeNode(child, doc);
    if (!clean) continue;
    ensure().bodyHtml += serialize(clean, doc);
  }
  return segments;
}

/**
 * canDeleteSummarySegment reports whether the current viewer may delete a segment:
 * parents may delete any; a kid may delete only their own (author-type=kid and
 * author-id = their own id). A pre-marker prefix (no sig) is never deletable.
 */
export function canDeleteSummarySegment(
  seg: SummarySegment,
  opts: { isKid: boolean; selfKidId: number | null },
): boolean {
  if (!seg.hasSign || seg.sig === "") return false;
  if (!opts.isKid) return true;
  return (
    seg.authorType === SUMMARY_AUTHOR_KID &&
    opts.selfKidId !== null &&
    seg.authorId === opts.selfKidId
  );
}
