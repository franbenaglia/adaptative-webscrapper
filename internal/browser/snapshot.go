package browser

import (
	"context"
	"encoding/json"
	"fmt"

	"adaptive-scraper/internal/domain"
)

// snapshotScript resume la página en candidatos numerados. Es fijo y no
// recibe nada del modelo. Agrupa elementos por un selector CSS derivado de su
// etiqueta y clases, se queda con los grupos repetidos y, dentro de cada
// elemento, localiza los textos y enlaces con un selector relativo que
// item.querySelector resuelve exactamente a ese nodo (igual que extractScript).
const snapshotScript = `() => {
  const MAX_ELEMENTS = 20000, MIN_ITEMS = 3, MAX_GROUPS = 6, MAX_FIELDS = 14;
  const MAX_SAMPLES = 3, SAMPLE_LENGTH = 70, INSPECTED_ITEMS = 40, MAX_NEXT = 5, TEXT_LENGTH = 1500;
  const SKIP = new Set(['SCRIPT', 'STYLE', 'NOSCRIPT', 'TEMPLATE', 'HEAD', 'META', 'LINK', 'BR', 'HR', 'OPTION', 'SVG', 'PATH']);

  const clean = (s) => (s || '').replace(/\s+/g, ' ').trim();
  const cut = (s, n) => (s.length > n ? s.slice(0, n - 1) + '…' : s);
  const visible = (el) => {
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) return false;
    const st = getComputedStyle(el);
    return st.visibility !== 'hidden' && st.display !== 'none';
  };
  const classes = (el) => [...el.classList].filter((c) => c.length < 40).slice(0, 3);
  const own = (el) => el.tagName.toLowerCase() + classes(el).map((c) => '.' + CSS.escape(c)).join('');
  const stableId = (el) => el.id && !/\d{3,}/.test(el.id);

  // Selector global: etiqueta y clases; sin clases, sube por los ancestros
  // hasta encontrar uno con clases o id.
  const globalSelector = (el) => {
    const parts = [];
    for (let cur = el, depth = 0; cur && cur !== document.body && depth < 4; cur = cur.parentElement, depth++) {
      if (cur !== el && stableId(cur)) { parts.unshift('#' + CSS.escape(cur.id)); break; }
      parts.unshift(own(cur));
      if (classes(cur).length > 0) break;
    }
    return parts.join(' > ');
  };

  // Selector relativo a item que querySelector resuelve exactamente a el.
  const relativeSelector = (item, el) => {
    if (el === item) return '';
    const nth = (node) => {
      const same = [...node.parentElement.children].filter((c) => c.tagName === node.tagName);
      return own(node) + ':nth-of-type(' + (same.indexOf(node) + 1) + ')';
    };
    const options = [own(el), nth(el)];
    const parent = el.parentElement;
    if (parent && parent !== item) {
      options.push(own(parent) + ' > ' + own(el), own(parent) + ' > ' + nth(el), nth(parent) + ' > ' + nth(el));
    }
    for (const sel of options) {
      try { if (item.querySelector(sel) === el) return sel; } catch (e) { /* selector no admitido */ }
    }
    return null;
  };

  const hasOwnText = (el) => [...el.childNodes].some((n) => n.nodeType === 3 && clean(n.textContent) !== '');

  const bySelector = new Map();
  const all = document.body ? document.body.querySelectorAll('*') : [];
  for (let i = 0; i < all.length && i < MAX_ELEMENTS; i++) {
    const el = all[i];
    if (SKIP.has(el.tagName.toUpperCase()) || !visible(el)) continue;
    const sel = globalSelector(el);
    if (!bySelector.has(sel)) bySelector.set(sel, 0);
    bySelector.set(sel, bySelector.get(sel) + 1);
  }

  let groups = [];
  for (const [selector, seen] of bySelector) {
    if (seen < MIN_ITEMS) continue;
    let items;
    try { items = [...document.querySelectorAll(selector)].filter(visible); } catch (e) { continue; }
    if (items.length < MIN_ITEMS) continue;

    const inspected = items.slice(0, INSPECTED_ITEMS);
    const fields = new Map();
    const add = (sel, attribute, value) => {
      value = clean(value);
      if (sel === null || value === '') return;
      const key = sel + '|' + attribute;
      if (!fields.has(key)) fields.set(key, { selector: sel, attribute, samples: [], items: new Set() });
      const f = fields.get(key);
      if (f.samples.length < MAX_SAMPLES && !f.samples.includes(cut(value, SAMPLE_LENGTH))) f.samples.push(cut(value, SAMPLE_LENGTH));
      return f;
    };
    inspected.forEach((item, index) => {
      const nodes = [item, ...item.querySelectorAll('*')];
      for (const el of nodes) {
        if (SKIP.has(el.tagName.toUpperCase())) continue;
        const sel = relativeSelector(item, el);
        if (sel === null) continue;
        if (hasOwnText(el)) { const f = add(sel, '', el.innerText || el.textContent); if (f) f.items.add(index); }
        if (el.tagName === 'A' && el.getAttribute('href')) { const f = add(sel, 'href', el.getAttribute('href')); if (f) f.items.add(index); }
        if (el.tagName === 'IMG' && el.getAttribute('src')) { const f = add(sel, 'src', el.getAttribute('src')); if (f) f.items.add(index); }
      }
    });
    const minCoverage = Math.max(2, Math.ceil(inspected.length * 0.3));
    const kept = [...fields.values()].filter((f) => f.items.size >= minCoverage).slice(0, MAX_FIELDS);
    if (kept.length === 0) continue;
    const width = Math.min(kept.length, 8);
    groups.push({ selector, count: items.length, first: items[0], items, fields: kept, score: items.length * width * width });
  }

  // De los grupos que se solapan (uno contiene al otro) gana el de más puntuación.
  groups.sort((a, b) => b.score - a.score || b.fields.length - a.fields.length);
  const accepted = [];
  for (const g of groups) {
    const overlaps = accepted.some((a) => a.items.some((i) => i.contains(g.first)) || g.items.some((i) => i.contains(a.first)));
    if (!overlaps) accepted.push(g);
    if (accepted.length === MAX_GROUPS) break;
  }

  const nextPattern = /(^|\b)(next|siguiente|sig\.|pr[oó]xim[oa]|older|more results|m[aá]s resultados)(\b|$)|^[›»→>]+$|^>>?$/i;
  const next = [];
  for (const el of document.querySelectorAll('a, button, [role="button"], input[type="submit"], input[type="button"]')) {
    if (next.length === MAX_NEXT) break;
    if (!visible(el)) continue;
    const label = clean(el.innerText || el.value || el.getAttribute('aria-label') || el.getAttribute('title'));
    const hints = [label, el.getAttribute('aria-label') || '', el.getAttribute('rel') || '', el.id, el.className && el.className.baseVal === undefined ? el.className : ''].join(' ');
    if (!nextPattern.test(label) && !/\b(next|siguiente)\b/i.test(hints)) continue;
    const tag = el.tagName.toLowerCase();
    const options = [];
    if (stableId(el)) options.push(tag + '#' + CSS.escape(el.id));
    if (el.getAttribute('rel')) options.push(tag + '[rel="' + CSS.escape(el.getAttribute('rel')) + '"]');
    if (classes(el).length > 0) options.push(own(el));
    if (el.getAttribute('aria-label')) options.push(tag + '[aria-label="' + CSS.escape(el.getAttribute('aria-label')) + '"]');
    options.push(globalSelector(el));
    const selector = options.find((sel) => { try { return document.querySelector(sel) === el; } catch (e) { return false; } });
    if (selector) next.push({ selector, text: cut(label || hints.trim(), SAMPLE_LENGTH) });
  }

  return JSON.stringify({
    url: location.href,
    title: cut(clean(document.title), 200),
    text: cut(clean(document.body ? document.body.innerText : ''), TEXT_LENGTH),
    groups: accepted.map((g, gi) => ({
      id: 'g' + (gi + 1),
      selector: g.selector,
      count: g.count,
      fields: g.fields.map((f, fi) => ({
        id: 'g' + (gi + 1) + '.f' + (fi + 1), selector: f.selector, attribute: f.attribute, samples: f.samples, coverage: f.items.size,
      })),
    })),
    next: next.map((n, ni) => ({ id: 'n' + (ni + 1), selector: n.selector, text: n.text })),
  });
}`

// blockScript reconoce páginas que impiden el acceso. Solo se considera
// bloqueo cuando la página tiene poco contenido propio, para no confundir un
// formulario de acceso en la cabecera de una página normal.
const blockScript = `() => {
  const text = ((document.body && document.body.innerText) || '').replace(/\s+/g, ' ').trim();
  if (text.length >= 1500) return '';
  const visible = (el) => {
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0 && getComputedStyle(el).visibility !== 'hidden';
  };
  const any = (selector) => [...document.querySelectorAll(selector)].some(visible);
  if (any('iframe[src*="recaptcha"], iframe[src*="hcaptcha"], iframe[src*="turnstile"], iframe[src*="challenges.cloudflare"], .g-recaptcha, .h-captcha, .cf-turnstile, [id*="captcha" i], [class*="captcha" i]')) {
    return 'la página muestra un CAPTCHA';
  }
  if (/(verify (that )?you are (a )?human|are you a robot|unusual traffic|access denied|checking your browser|verifica que eres humano|no soy un robot|acceso denegado)/i.test(text)) {
    return 'la página pide una verificación humana o deniega el acceso';
  }
  if (any('input[type="password"]')) return 'la página exige iniciar sesión';
  return '';
}`

func (s *session) Snapshot(ctx context.Context) (domain.PageSnapshot, error) {
	var snapshot domain.PageSnapshot
	out, err := s.page.Evaluate(snapshotScript)
	if err != nil {
		return snapshot, ctxErr(ctx, err)
	}
	raw, ok := out.(string)
	if !ok {
		return snapshot, fmt.Errorf("resultado inesperado del resumen de página: %T", out)
	}
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return snapshot, fmt.Errorf("resumen de página ilegible: %w", err)
	}
	return snapshot, nil
}

func (s *session) BlockReason(ctx context.Context) (string, error) {
	out, err := s.page.Evaluate(blockScript)
	if err != nil {
		return "", ctxErr(ctx, err)
	}
	reason, _ := out.(string)
	return reason, nil
}
