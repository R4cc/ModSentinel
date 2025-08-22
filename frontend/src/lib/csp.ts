export function applyStyleNonce() {
  const meta = document.querySelector('meta[name="csp-nonce"]');
  const nonce = meta?.getAttribute("content");
  if (!nonce) return;
  document.querySelectorAll("style").forEach((el) => {
    if (!el.getAttribute("nonce")) {
      el.setAttribute("nonce", nonce);
    }
  });
  const obs = new MutationObserver((muts) => {
    for (const m of muts) {
      m.addedNodes.forEach((node) => {
        if (node instanceof HTMLStyleElement && !node.getAttribute("nonce")) {
          node.setAttribute("nonce", nonce);
        }
      });
    }
  });
  obs.observe(document.head, { childList: true });
}
