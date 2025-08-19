import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import usePreferences from '@/hooks/usePreferences.js';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card.jsx';
import { Select } from '@/components/ui/Select.jsx';
import { Input } from '@/components/ui/Input.jsx';
import { Button } from '@/components/ui/Button.jsx';
import { getToken, saveToken, clearToken } from '@/lib/api.ts';

export default function Settings() {
  const { theme, setTheme, interval, setInterval } = usePreferences();
  const [token, setToken] = useState('');
  const [show, setShow] = useState(false);

  useEffect(() => {
    getToken().then(setToken).catch(() => {});
  }, []);

  async function handleSave() {
    if (!token) {
      toast.error('Token cannot be empty');
      return;
    }
    try {
      await saveToken(token);
      toast.success('Token saved');
    } catch {
      toast.error('Failed to save token');
    }
  }

  async function handleClear() {
    try {
      await clearToken();
      setToken('');
      toast.success('Token cleared');
    } catch {
      toast.error('Failed to clear token');
    }
  }

  return (
    <div className="max-w-md space-y-lg">
      <Card>
        <CardHeader>
          <CardTitle>Preferences</CardTitle>
        </CardHeader>
        <CardContent className="space-y-md">
          <div className="space-y-xs">
            <label htmlFor="theme" className="text-sm font-medium">
              Theme
            </label>
            <Select id="theme" value={theme} onChange={(e) => setTheme(e.target.value)}>
              <option value="light">Light</option>
              <option value="dark">Dark</option>
              <option value="system">System</option>
            </Select>
          </div>
          <div className="space-y-xs">
            <label htmlFor="interval" className="text-sm font-medium">
              Update check
            </label>
            <Select
              id="interval"
              value={interval}
              onChange={(e) => setInterval(e.target.value)}
            >
              <option value="manual">Manual</option>
              <option value="hourly">Hourly</option>
              <option value="daily">Daily</option>
              <option value="weekly">Weekly</option>
            </Select>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Modrinth API Token</CardTitle>
        </CardHeader>
        <CardContent className="space-y-md">
          <div className="space-y-xs">
            <label htmlFor="token" className="text-sm font-medium">
              Token{' '}
              <a
                href="https://docs.modrinth.com/api/"
                target="_blank"
                rel="noreferrer"
                className="text-primary underline"
              >
                Docs
              </a>
            </label>
            <div className="flex gap-sm">
              <Input
                id="token"
                type={show ? 'text' : 'password'}
                value={token}
                onChange={(e) => setToken(e.target.value)}
                className="flex-1"
              />
              <Button variant="secondary" onClick={() => setShow((s) => !s)}>
                {show ? 'Hide' : 'Show'}
              </Button>
            </div>
          </div>
          <div className="flex gap-sm">
            <Button onClick={handleSave}>Save</Button>
            <Button variant="secondary" onClick={handleClear} disabled={!token}>
              Clear
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}