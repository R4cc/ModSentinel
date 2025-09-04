import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App.jsx";
import ErrorBoundary from "@/components/ErrorBoundary.jsx";
import { applyStyleNonce } from "@/lib/csp.ts";
import "./index.css";
import { useEffect } from "react";
import { useMetaStore } from "@/stores/metaStore.js";
import { toast } from "@/lib/toast.ts";

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

function Bootstrapper({ children }) {
  const load = useMetaStore((s) => s.load);
  const loaded = useMetaStore((s) => s.loaded);
  const error = useMetaStore((s) => s.error);
  useEffect(() => {
    if (!loaded) {
      load().then((ok) => { if (!ok) toast.error("Failed to load loaders metadata"); });
    }
  }, [loaded, load]);
  useEffect(() => {
    if (error) toast.error(error);
  }, [error]);
  return children;
}

ReactDOM.createRoot(document.getElementById("app")).render(
  <React.StrictMode>
    <BrowserRouter>
      <ErrorBoundary>
        <Bootstrapper>
          <App />
        </Bootstrapper>
      </ErrorBoundary>
    </BrowserRouter>
  </React.StrictMode>,
);
