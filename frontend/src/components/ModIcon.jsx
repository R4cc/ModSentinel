import { useState, useMemo } from "react";
import { Package } from "lucide-react";

export default function ModIcon({ url, cacheKey }) {
  const [error, setError] = useState(false);

  const { src, remote } = useMemo(() => {
    if (!url) {
      return { src: "", remote: false };
    }
    try {
      const u = new URL(url, window.location.origin);
      if (cacheKey) {
        u.searchParams.set("v", cacheKey);
      }
      return { src: u.toString(), remote: u.origin !== window.location.origin };
    } catch {
      return { src: "", remote: false };
    }
  }, [url, cacheKey]);

  if (!src || error) {
    return (
      <Package
        className="h-6 w-6 text-muted-foreground"
        aria-hidden="true"
        data-testid="icon-placeholder"
      />
    );
  }

  return (
    <img
      src={src}
      alt=""
      className="h-6 w-6 rounded-sm"
      loading="lazy"
      crossOrigin={remote ? "anonymous" : undefined}
      onError={() => setError(true)}
    />
  );
}
