import { test, expect } from "vitest";
import { applyStyleNonce } from "./csp";

test("applies nonce to new style tags", () => {
  const meta = document.createElement("meta");
  meta.setAttribute("name", "csp-nonce");
  meta.setAttribute("content", "abc");
  document.head.appendChild(meta);
  const style = document.createElement("style");
  document.head.appendChild(style);
  applyStyleNonce();
  expect(style.getAttribute("nonce")).toBe("abc");
});
