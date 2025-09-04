import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App.jsx";
import ErrorBoundary from "@/components/ErrorBoundary.jsx";
import { applyStyleNonce } from "@/lib/csp.ts";
import "./index.css";

// Auto-apply dark mode based on system preference
try {
  const mq = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)');
  const apply = () => {
    if (!mq) return;
    document.documentElement.classList.toggle('dark', mq.matches);
  };
  apply();
  mq?.addEventListener?.('change', apply);
} catch {}

applyStyleNonce();

ReactDOM.createRoot(document.getElementById("app")).render(
  <React.StrictMode>
    <BrowserRouter>
      <ErrorBoundary>
        <App />
      </ErrorBoundary>
    </BrowserRouter>
  </React.StrictMode>,
);
