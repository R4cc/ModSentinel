import { useEffect, useState } from 'react';

const defaults = { theme: 'system', interval: 'manual' };

export default function usePreferences() {
  const initial = (() => {
    try {
      const stored = JSON.parse(localStorage.getItem('prefs')) || {};
      return { ...defaults, ...stored };
    } catch {
      return defaults;
    }
  })();

  const [theme, setTheme] = useState(initial.theme);
  const [interval, setInterval] = useState(initial.interval);

  useEffect(() => {
    localStorage.setItem('prefs', JSON.stringify({ theme, interval }));
  }, [theme, interval]);

  useEffect(() => {
    const root = document.documentElement;
    const media = window.matchMedia('(prefers-color-scheme: dark)');

    const apply = () => {
      const system = media.matches ? 'dark' : 'light';
      const resolved = theme === 'system' ? system : theme;
      root.classList.toggle('dark', resolved === 'dark');
    };

    apply();
    if (theme === 'system') {
      media.addEventListener('change', apply);
      return () => media.removeEventListener('change', apply);
    }
  }, [theme]);

  return { theme, setTheme, interval, setInterval };
}

